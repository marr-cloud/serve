package rules

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return dir
}

func TestLoad_NoConfigReturnsEmptySet(t *testing.T) {
	s, err := Load("", t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s == nil {
		t.Fatal("nil Set")
	}
	if s.Public() != "" {
		t.Fatalf("empty Set Public() %q, want empty", s.Public())
	}
	if len(s.Redirects()) != 0 || len(s.Rewrites()) != 0 || len(s.Headers()) != 0 {
		t.Fatal("empty Set should have no rules")
	}
}

func TestLoad_ServeJson(t *testing.T) {
	dir := writeTemp(t, "serve.json", `{
		"public": "./dist",
		"redirects": [{"source": "/old", "destination": "/new"}],
		"rewrites": [{"source": "/api/:id", "destination": "/data/:id.json"}],
		"headers": [{"source": "/*.css", "headers": [{"key": "Cache-Control", "value": "max-age=60"}]}],
		"cleanUrls": true,
		"trailingSlash": false,
		"directoryListing": true,
		"unlisted": [".git"],
		"renderSingle": false,
		"symlinks": false
	}`)
	s, err := Load("", dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Public() != "./dist" {
		t.Fatalf("Public %q", s.Public())
	}
	if len(s.Redirects()) != 1 {
		t.Fatalf("got %d redirects", len(s.Redirects()))
	}
}

func TestLoad_LegacyNowJson(t *testing.T) {
	dir := writeTemp(t, "now.json", `{"public": "./out"}`)
	s, err := Load("", dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Public() != "./out" {
		t.Fatalf("Public %q", s.Public())
	}
}

func TestLoad_ExplicitPathWinsOverDirAuto(t *testing.T) {
	dir := writeTemp(t, "serve.json", `{"public": "./auto"}`)
	other := filepath.Join(t.TempDir(), "custom.json")
	if err := os.WriteFile(other, []byte(`{"public": "./custom"}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s, err := Load(other, dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Public() != "./custom" {
		t.Fatalf("Public %q want ./custom", s.Public())
	}
}

func TestLoad_RejectsUnknownKey(t *testing.T) {
	dir := writeTemp(t, "serve.json", `{"redirectz": []}`)
	if _, err := Load("", dir); err == nil {
		t.Fatal("expected error for unknown key 'redirectz'")
	}
}

func TestLoad_InvalidPatternErrors(t *testing.T) {
	dir := writeTemp(t, "serve.json", `{"redirects": [{"source": "", "destination": "/x"}]}`)
	if _, err := Load("", dir); err == nil {
		t.Fatal("expected error for empty source pattern")
	}
}
