// Package users discovers login usernames for owner-filter autocomplete. It
// prefers an Active Directory query over GSSAPI (ldapsearch) and falls back to
// getent passwd / /etc/passwd when LDAP is unavailable.
package users

import (
	"bufio"
	"context"
	"encoding/base64"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// minUID excludes system/daemon accounts in the getent fallback; real login
// users are >= 1000 on typical Linux farm nodes.
const minUID = 1000

// ldapFilter selects enabled person accounts (excludes disabled accounts via
// the userAccountControl bit-and rule).
const ldapFilter = "(&(objectClass=user)(objectCategory=person)(!(userAccountControl:1.2.840.113556.1.4.803:=2)))"

const cmdTimeout = 15 * time.Second

// excluded lists account names to drop from autocomplete regardless of source.
var excluded = map[string]struct{}{
	"Administrator": {},
}

// Config controls how usernames are discovered from the directory.
type Config struct {
	URI       string // ldapsearch -H value, e.g. "ldap://dc02.example.com"
	BaseDN    string // search base, e.g. "dc=example,dc=com"
	Realm     string // Kerberos realm for the kinit principal, e.g. "EXAMPLE.COM"
	Username  string // login name; used as the kinit principal
	Password  string // used only when AutoKinit is enabled
	AutoKinit bool   // attempt kinit with Password when no ticket exists
}

// List returns sorted, de-duplicated login usernames. It prefers an LDAP
// (Active Directory) query over GSSAPI; if that yields nothing it falls back to
// getent passwd. Any failure degrades gracefully to an empty slice, leaving the
// caller to rely on other autocomplete sources (e.g. job owners).
func List(cfg Config) []string {
	if names := fromLDAP(cfg); len(names) > 0 {
		return names
	}
	return fromGetent()
}

// fromLDAP runs ldapsearch over GSSAPI and parses the sAMAccountName values.
func fromLDAP(cfg Config) []string {
	if cfg.URI == "" || cfg.BaseDN == "" {
		return nil
	}
	if !ensureTicket(cfg) {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ldapsearch",
		"-H", cfg.URI,
		"-Y", "GSSAPI",
		"-b", cfg.BaseDN,
		ldapFilter,
		"sAMAccountName",
	)
	// Output() still returns whatever reached stdout even when ldapsearch exits
	// non-zero, so parse it regardless of the error. SASL status lines go to
	// stderr and never appear here.
	out, _ := cmd.Output()
	return parseLDAP(string(out))
}

// ensureTicket reports whether a usable Kerberos ticket is available. When
// AutoKinit is enabled and no ticket exists it attempts a single kinit using
// the supplied password. It never blocks on an interactive password prompt.
//
// AutoKinit is opt-in because running kinit with a wrong password can trip an
// Active Directory account-lockout policy; on a Kerberos-backed farm a ticket
// is normally already present (via SSSD/login) and this just returns true.
func ensureTicket(cfg Config) bool {
	if HasTicket() {
		return true
	}
	if !cfg.AutoKinit || cfg.Username == "" || cfg.Realm == "" || cfg.Password == "" {
		return false
	}
	if err := Kinit(cfg.Username+"@"+cfg.Realm, cfg.Password); err != nil {
		return false
	}
	return HasTicket()
}

// HasTicket reports whether the credential cache holds a valid Kerberos ticket.
func HasTicket() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "klist", "-s").Run() == nil
}

// Kinit runs `kinit <principal>`, feeding password on stdin. It returns an
// error if kinit is missing, times out, or rejects the password.
//
// Caution: a wrong password submitted to kinit can count against an Active
// Directory account-lockout policy. Callers should only invoke this with a
// password the user supplied deliberately (or one already known good).
func Kinit(principal, password string) error {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "kinit", principal)
	cmd.Stdin = strings.NewReader(password + "\n")
	return cmd.Run()
}

// parseLDAP extracts sAMAccountName values from ldapsearch LDIF output. Other
// LDIF attributes and SASL status lines are ignored, and base64-encoded values
// ("sAMAccountName::") are decoded.
func parseLDAP(text string) []string {
	seen := make(map[string]struct{})
	var names []string

	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())

		var val string
		switch {
		case strings.HasPrefix(line, "sAMAccountName::"):
			enc := strings.TrimSpace(line[len("sAMAccountName::"):])
			dec, err := base64.StdEncoding.DecodeString(enc)
			if err != nil {
				continue
			}
			val = string(dec)
		case strings.HasPrefix(line, "sAMAccountName:"):
			val = strings.TrimSpace(line[len("sAMAccountName:"):])
		default:
			continue
		}

		val = strings.TrimSpace(val)
		if val == "" {
			continue
		}
		if _, skip := excluded[val]; skip {
			continue
		}
		if _, dup := seen[val]; dup {
			continue
		}
		seen[val] = struct{}{}
		names = append(names, val)
	}

	sort.Strings(names)
	return names
}

// fromGetent lists usernames via getent passwd (LDAP/NSS or /etc/passwd),
// keeping only UID >= minUID. Used as a fallback when LDAP is unavailable.
func fromGetent() []string {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "getent", "passwd").Output()
	if err != nil {
		return nil
	}
	return parsePasswd(string(out))
}

// parsePasswd extracts usernames from passwd-format text, keeping only entries
// with UID >= minUID. Lines are "name:passwd:uid:gid:gecos:home:shell".
func parsePasswd(text string) []string {
	seen := make(map[string]struct{})
	var names []string

	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		line := sc.Text()
		fields := strings.Split(line, ":")
		if len(fields) < 3 {
			continue
		}
		name := fields[0]
		if name == "" {
			continue
		}
		uid, err := strconv.Atoi(fields[2])
		if err != nil || uid < minUID {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}

	sort.Strings(names)
	return names
}
