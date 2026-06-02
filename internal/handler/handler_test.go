package handler

import (
	"bytes"
	stdgzip "compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"serve/internal/config"
)

func mkFS() fstest.MapFS {
	mod := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	return fstest.MapFS{
		"index.html":     &fstest.MapFile{Data: []byte("<html>root</html>"), ModTime: mod},
		"app.js":         &fstest.MapFile{Data: []byte(strings.Repeat("var x = 1;\n", 200)), ModTime: mod},
		"logo.png":       &fstest.MapFile{Data: []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("x", 2000)), ModTime: mod},
		"sub/index.html": &fstest.MapFile{Data: []byte("<html>sub</html>"), ModTime: mod},
		"docs/readme":    &fstest.MapFile{Data: []byte("plain text content " + strings.Repeat("y", 2000)), ModTime: mod},
	}
}

func newHandler(cfg config.Config, fsys fstest.MapFS) http.Handler {
	return New(cfg, fsys)
}

func TestServe_File200(t *testing.T) {
	h := newHandler(config.Config{}, mkFS())
	req := httptest.NewRequest("GET", "/index.html", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("Content-Type %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("ETag") == "" {
		t.Fatal("missing ETag")
	}
}

func TestServe_IfNoneMatch304(t *testing.T) {
	h := newHandler(config.Config{}, mkFS())
	r1 := httptest.NewRequest("GET", "/index.html", nil)
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, r1)
	etag := w1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag")
	}
	r2 := httptest.NewRequest("GET", "/index.html", nil)
	r2.Header.Set("If-None-Match", etag)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d", w2.Code)
	}
	if w2.Body.Len() != 0 {
		t.Fatal("304 must not have a body")
	}
}

func TestServe_GzipForJS(t *testing.T) {
	h := newHandler(config.Config{}, mkFS())
	req := httptest.NewRequest("GET", "/app.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding %q", rec.Header().Get("Content-Encoding"))
	}
	if rec.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("Vary %q", rec.Header().Get("Vary"))
	}
	gr, err := stdgzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	got, _ := io.ReadAll(gr)
	if !strings.HasPrefix(string(got), "var x = 1;") {
		t.Fatal("decompressed content mismatch")
	}
}

func TestServe_NoGzipForPNG(t *testing.T) {
	h := newHandler(config.Config{}, mkFS())
	req := httptest.NewRequest("GET", "/logo.png", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("PNG should not be gzipped, got %q", rec.Header().Get("Content-Encoding"))
	}
}

func TestServe_RangeDisablesGzip(t *testing.T) {
	h := newHandler(config.Config{}, mkFS())
	req := httptest.NewRequest("GET", "/app.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Range", "bytes=0-49")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status %d, want 206", rec.Code)
	}
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("Content-Encoding %q (must be empty when Range is set)", rec.Header().Get("Content-Encoding"))
	}
	if rec.Body.Len() != 50 {
		t.Fatalf("body length %d, want 50", rec.Body.Len())
	}
}

func TestServe_DirectoryIndex(t *testing.T) {
	h := newHandler(config.Config{}, mkFS())
	req := httptest.NewRequest("GET", "/sub/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "sub") {
		t.Fatal("expected sub/index.html content")
	}
}

func TestServe_DirectoryListingWhenNoIndex(t *testing.T) {
	h := newHandler(config.Config{}, mkFS())
	req := httptest.NewRequest("GET", "/docs/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Index of") {
		t.Fatal("expected directory listing")
	}
}

func TestServe_SPA_HTMLAccept(t *testing.T) {
	h := newHandler(config.Config{Single: true}, mkFS())
	req := httptest.NewRequest("GET", "/some/route", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "root") {
		t.Fatal("SPA should serve root index.html")
	}
}

func TestServe_SPA_JSONNoFallback(t *testing.T) {
	h := newHandler(config.Config{Single: true}, mkFS())
	req := httptest.NewRequest("GET", "/api/missing", nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}
}

func TestServe_PathTraversal(t *testing.T) {
	h := newHandler(config.Config{}, mkFS())
	req := httptest.NewRequest("GET", "/../etc/passwd", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403 or 404", rec.Code)
	}
}

func TestServe_DirectoryRedirectsToTrailingSlash(t *testing.T) {
	h := newHandler(config.Config{}, mkFS())
	req := httptest.NewRequest("GET", "/sub", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status %d, want 301", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/sub/" {
		t.Fatalf("Location %q, want /sub/", loc)
	}
}

func TestServe_IfNoneMatchMultiValue(t *testing.T) {
	h := newHandler(config.Config{}, mkFS())
	r1 := httptest.NewRequest("GET", "/index.html", nil)
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, r1)
	etag := w1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag")
	}
	r2 := httptest.NewRequest("GET", "/index.html", nil)
	r2.Header.Set("If-None-Match", `"other-etag", `+etag)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Code != http.StatusNotModified {
		t.Fatalf("expected 304 for multi-value match, got %d", w2.Code)
	}
}

func TestServe_IfNoneMatchWildcard(t *testing.T) {
	h := newHandler(config.Config{}, mkFS())
	req := httptest.NewRequest("GET", "/index.html", nil)
	req.Header.Set("If-None-Match", "*")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("expected 304 for *, got %d", rec.Code)
	}
}

func TestServe_TraversalEmbedded(t *testing.T) {
	h := newHandler(config.Config{}, mkFS())
	req := httptest.NewRequest("GET", "/sub/../../etc/passwd", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403 or 404 (raw .. should be rejected)", rec.Code)
	}
}

func TestServe_OptionsWithCORS(t *testing.T) {
	h := newHandler(config.Config{CORS: true}, mkFS())
	req := httptest.NewRequest("OPTIONS", "/index.html", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d, want 204", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("expected ACAO *")
	}
}
