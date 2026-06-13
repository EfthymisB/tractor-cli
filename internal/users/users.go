// Package users discovers login usernames from the local system directory
// (LDAP/NSS via getent, or /etc/passwd as a fallback) for owner autocomplete.
package users

import (
	"bufio"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// minUID excludes system/daemon accounts; real LDAP/login users are >= 1000
// on typical Linux farm nodes.
const minUID = 1000

// List returns sorted, de-duplicated login usernames discovered via
// `getent passwd`. On platforms without getent (e.g. Windows dev machines) or
// on any error it returns an empty slice — callers degrade gracefully to no
// autocomplete.
func List() []string {
	out, err := exec.Command("getent", "passwd").Output()
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
