package handler

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestServeDirectory_EscapesAndSorts(t *testing.T) {
	mod := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	fsys := fstest.MapFS{
		"sub/zeta":              &fstest.MapFile{ModTime: mod, Mode: 0o755},
		"sub/alpha.txt":         &fstest.MapFile{Data: []byte("a"), ModTime: mod},
		"sub/<script>.txt":      &fstest.MapFile{Data: []byte("x"), ModTime: mod},
		"sub/folder/.keep":      &fstest.MapFile{Data: []byte(""), ModTime: mod},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/sub/", nil)
	if err := serveDirectory(rec, req, fsys, "sub", "/sub/", nil); err != nil {
		t.Fatalf("serveDirectory: %v", err)
	}
	body, _ := io.ReadAll(rec.Body)
	html := string(body)

	if rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type: %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("Cache-Control: %q", rec.Header().Get("Cache-Control"))
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("X-Content-Type-Options missing")
	}
	if strings.Contains(html, "<script>.txt") {
		t.Fatal("unescaped < in directory listing (XSS risk)")
	}
	if !strings.Contains(html, "&lt;script&gt;.txt") {
		t.Fatal("expected escaped < in directory listing")
	}
	idxFolder := strings.Index(html, "folder/")
	idxAlpha := strings.Index(html, "alpha.txt")
	if idxFolder < 0 || idxAlpha < 0 || idxFolder > idxAlpha {
		t.Fatal("expected folders listed before files, both present")
	}
}

func TestServeDirectory_URLEncodesFilenames(t *testing.T) {
	mod := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	fsys := fstest.MapFS{
		"d/my file.txt":   &fstest.MapFile{Data: []byte("x"), ModTime: mod},
		"d/q?weird=1.txt": &fstest.MapFile{Data: []byte("y"), ModTime: mod},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/d/", nil)
	if err := serveDirectory(rec, req, fsys, "d", "/d/", nil); err != nil {
		t.Fatalf("serveDirectory: %v", err)
	}
	body, _ := io.ReadAll(rec.Body)
	out := string(body)
	if !strings.Contains(out, "my%20file.txt") {
		t.Fatalf("expected url-encoded space (%%20) in href, got:\n%s", out)
	}
	if !strings.Contains(out, "q%3Fweird=1.txt") {
		t.Fatalf("expected url-encoded ? (%%3F) in href, got:\n%s", out)
	}
	// Display text should still be unencoded (HTML-escaped only).
	if !strings.Contains(out, "my file.txt") {
		t.Fatal("display name should remain literal (only href is encoded)")
	}
}
