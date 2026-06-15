package session

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPath(t *testing.T) {
	p := Path("engine01", 80, "alice")
	if !strings.Contains(p, filepath.Join("sites", "engine01@80")) {
		t.Errorf("Path missing site dir: %q", p)
	}
	if !strings.HasSuffix(p, ".alice.session") {
		t.Errorf("Path missing user-suffixed filename: %q", p)
	}
	if !strings.Contains(filepath.Base(p), appName+".") {
		t.Errorf("Path filename missing app prefix: %q", p)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sites", "h@80", "tractor-cli.host.bob.session")
	if _, ok := Load(p); ok {
		t.Fatal("Load on missing file should report ok=false")
	}
	if err := Save(p, "TSID-123"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, ok := Load(p)
	if !ok || got != "TSID-123" {
		t.Fatalf("Load after Save = %q,%v want TSID-123,true", got, ok)
	}
	Delete(p)
	if _, ok := Load(p); ok {
		t.Fatal("Load after Delete should report ok=false")
	}
}
