// Package session persists the Tractor engine session token (tsid) to disk so
// the password only needs to be entered once, then reused until the engine
// invalidates it.
//
// The on-disk layout mirrors Tractor's own EngineClient session files
// (~/.pixarPrefs/Tractor/sites/<host>@<port>/<app>.<clienthost>.<user>.session
// containing {"tsid": "..."}), so the path and format are familiar to anyone
// who has used Pixar's tq/tractor tools. See tractor_source/base/EngineClient.py
// (sessionFilename / writeSessionFile / canReuseSession).
package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// appName is the leading component of the session filename, distinguishing our
// sessions from Pixar's own tools sharing the same sites directory.
const appName = "tractor-cli"

// sessionDoc is the JSON document stored on disk. Matching Tractor's format
// ({"tsid": "..."}) keeps the files interchangeable.
type sessionDoc struct {
	TSID string `json:"tsid"`
}

// prefsDir returns the RenderMan preferences directory that anchors session
// files. On Linux (the only place this app talks to an engine) that is
// $HOME/.pixarPrefs/Tractor, matching EngineClient.rendermanPrefsDir().
func prefsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.TempDir()
	}
	return filepath.Join(home, ".pixarPrefs", "Tractor")
}

// Path returns the session-file path for the given engine and user, mirroring
// Tractor's sessionFilename(): .../sites/<host>@<port>/<app>.<clienthost>.<user>.session.
func Path(host string, port int, user string) string {
	clientHost, err := os.Hostname()
	if err != nil || clientHost == "" {
		clientHost = "localhost"
	}
	site := fmt.Sprintf("%s@%d", host, port)
	name := fmt.Sprintf("%s.%s.%s.session", appName, clientHost, user)
	return filepath.Join(prefsDir(), "sites", site, name)
}

// Load reads a saved tsid from path. ok is false if the file is missing or
// unreadable (a normal first-run condition, not an error).
func Load(path string) (tsid string, ok bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var doc sessionDoc
	if err := json.Unmarshal(b, &doc); err != nil || doc.TSID == "" {
		return "", false
	}
	return doc.TSID, true
}

// Save writes tsid to path (creating parent dirs), with owner-only permissions
// (0700 dir / 0600 file) since the tsid grants engine access.
func Save(path, tsid string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating session dir: %w", err)
	}
	b, err := json.Marshal(sessionDoc{TSID: tsid})
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing session file: %w", err)
	}
	return nil
}

// Delete removes a session file. A missing file is not an error.
func Delete(path string) {
	_ = os.Remove(path)
}
