package handler

import (
	"bytes"
	stdgzip "compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/andybalholm/brotli"

	"serve/internal/config"
	"serve/internal/rules"
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
	return New(cfg, fsys, nil)
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

func TestServe_BrotliForJS(t *testing.T) {
	h := newHandler(config.Config{}, mkFS())
	req := httptest.NewRequest("GET", "/app.js", nil)
	req.Header.Set("Accept-Encoding", "br")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Header().Get("Content-Encoding") != "br" {
		t.Fatalf("Content-Encoding %q", rec.Header().Get("Content-Encoding"))
	}
	if rec.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("Vary %q", rec.Header().Get("Vary"))
	}
	got, err := io.ReadAll(brotli.NewReader(rec.Body))
	if err != nil {
		t.Fatalf("brotli decode: %v", err)
	}
	if !strings.HasPrefix(string(got), "var x = 1;") {
		t.Fatal("decoded prefix wrong")
	}
}

func TestServe_PreferBrotliOverGzip(t *testing.T) {
	h := newHandler(config.Config{}, mkFS())
	req := httptest.NewRequest("GET", "/app.js", nil)
	req.Header.Set("Accept-Encoding", "gzip, br")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Content-Encoding") != "br" {
		t.Fatalf("Content-Encoding %q, want br", rec.Header().Get("Content-Encoding"))
	}
}

func TestServe_RangeDisablesBrotli(t *testing.T) {
	h := newHandler(config.Config{}, mkFS())
	req := httptest.NewRequest("GET", "/app.js", nil)
	req.Header.Set("Accept-Encoding", "br")
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

func mustRuleSet(t *testing.T, body string) *rules.Set {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "serve.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s, err := rules.Load("", dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return s
}

func TestRules_RedirectFires(t *testing.T) {
	set := mustRuleSet(t, `{"redirects":[{"source":"/old","destination":"/new"}]}`)
	h := New(config.Config{}, mkFS(), set)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/old", nil))
	if rec.Code != 301 || rec.Header().Get("Location") != "/new" {
		t.Fatalf("got %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestRules_RewriteServesAliasedFile(t *testing.T) {
	set := mustRuleSet(t, `{"rewrites":[{"source":"/api/:id","destination":"/api/:id.json"}]}`)
	fsys := fstest.MapFS{"api/42.json": &fstest.MapFile{Data: []byte(`{"id":42}`)}}
	h := New(config.Config{}, fsys, set)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/42", nil))
	if rec.Code != 200 || rec.Body.String() != `{"id":42}` {
		t.Fatalf("got %d body=%q", rec.Code, rec.Body.String())
	}
}

func TestRules_CleanUrlsServeHTML(t *testing.T) {
	set := mustRuleSet(t, `{"cleanUrls": true}`)
	fsys := fstest.MapFS{"about.html": &fstest.MapFile{Data: []byte("<h1>about</h1>")}}
	// SetExists must close over fsys; the handler does this in cmd/serve. For
	// tests we wire it explicitly:
	set.SetExists(func(p string) bool {
		_, err := fsys.Open(stripLeadingSlash(p))
		return err == nil
	})
	h := New(config.Config{}, fsys, set)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/about", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "about") {
		t.Fatalf("got %d body=%q", rec.Code, rec.Body.String())
	}
}

func TestRules_CleanUrlsRedirectsHTMLSuffix(t *testing.T) {
	set := mustRuleSet(t, `{"cleanUrls": true}`)
	fsys := fstest.MapFS{"about.html": &fstest.MapFile{Data: []byte("<h1>about</h1>")}}
	set.SetExists(func(p string) bool {
		_, err := fsys.Open(stripLeadingSlash(p))
		return err == nil
	})
	h := New(config.Config{}, fsys, set)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/about.html", nil))
	if rec.Code != 301 {
		t.Fatalf("status %d, want 301", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/about" {
		t.Fatalf("Location %q, want /about", loc)
	}
}

func TestRules_TrailingSlashStripOverridesF1Default(t *testing.T) {
	set := mustRuleSet(t, `{"trailingSlash": false}`)
	h := New(config.Config{}, mkFS(), set)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/sub/", nil))
	if rec.Code != 301 || rec.Header().Get("Location") != "/sub" {
		t.Fatalf("got %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestRules_HeadersInjected(t *testing.T) {
	set := mustRuleSet(t, `{"headers":[{"source":"/**","headers":[{"key":"Cache-Control","value":"max-age=10"}]}]}`)
	h := New(config.Config{}, mkFS(), set)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/index.html", nil))
	if got := rec.Header().Get("Cache-Control"); got != "max-age=10" {
		t.Fatalf("Cache-Control %q", got)
	}
}

func TestRules_DirectoryListingDisabled404(t *testing.T) {
	set := mustRuleSet(t, `{"directoryListing": false}`)
	h := New(config.Config{}, mkFS(), set)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/docs/", nil))
	if rec.Code != 404 {
		t.Fatalf("status %d, want 404", rec.Code)
	}
}

func TestRules_UnlistedHidesFromListing(t *testing.T) {
	mod := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	fsys := fstest.MapFS{
		"area/visible.txt": &fstest.MapFile{Data: []byte("v"), ModTime: mod},
		"area/secret.txt":  &fstest.MapFile{Data: []byte("s"), ModTime: mod},
	}
	set := mustRuleSet(t, `{"unlisted": ["secret.txt"]}`)
	h := New(config.Config{}, fsys, set)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/area/", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "visible.txt") || strings.Contains(body, "secret.txt") {
		t.Fatalf("listing should hide secret.txt; got:\n%s", body)
	}
}

func TestRules_RenderSingleServesLoneFile(t *testing.T) {
	mod := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	fsys := fstest.MapFS{
		"lone/only.txt": &fstest.MapFile{Data: []byte("hello"), ModTime: mod},
	}
	set := mustRuleSet(t, `{"renderSingle": true}`)
	h := New(config.Config{}, fsys, set)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/lone/", nil))
	if rec.Code != 200 || rec.Body.String() != "hello" {
		t.Fatalf("got %d body=%q", rec.Code, rec.Body.String())
	}
}

func TestRules_NilSetUnchanged(t *testing.T) {
	h := New(config.Config{}, mkFS(), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/index.html", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
}

// helper used by TestRules_CleanUrlsServeHTML
func stripLeadingSlash(p string) string {
	if len(p) > 0 && p[0] == '/' {
		return p[1:]
	}
	return p
}
