package rules

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func mustCompile(t *testing.T, src string) *Pattern {
	t.Helper()
	p, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	return p
}

func TestPre_RedirectSimple(t *testing.T) {
	s := &Set{redirects: []Redirect{
		{Pattern: mustCompile(t, "/old"), Destination: "/new", Status: 301},
	}}
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next should not be called")
	}))
	req := httptest.NewRequest("GET", "/old", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 301 {
		t.Fatalf("status %d, want 301", rec.Code)
	}
	if rec.Header().Get("Location") != "/new" {
		t.Fatalf("Location %q", rec.Header().Get("Location"))
	}
}

func TestPre_RedirectWithCapture(t *testing.T) {
	s := &Set{redirects: []Redirect{
		{Pattern: mustCompile(t, "/u/:id"), Destination: "/users/:id", Status: 301},
	}}
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest("GET", "/u/42", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Location") != "/users/42" {
		t.Fatalf("Location %q, want /users/42", rec.Header().Get("Location"))
	}
}

func TestPre_RedirectStatusOverride(t *testing.T) {
	s := &Set{redirects: []Redirect{
		{Pattern: mustCompile(t, "/x"), Destination: "/y", Status: 308},
	}}
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != 308 {
		t.Fatalf("status %d, want 308", rec.Code)
	}
}

func TestPre_NoMatchCallsNext(t *testing.T) {
	s := &Set{redirects: []Redirect{
		{Pattern: mustCompile(t, "/old"), Destination: "/new", Status: 301},
	}}
	called := false
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/other", nil))
	if !called {
		t.Fatal("expected next handler to be called for non-matching URL")
	}
}

func TestPre_RewriteMutatesURL(t *testing.T) {
	s := &Set{rewrites: []Rewrite{
		{Pattern: mustCompile(t, "/api/:id"), Destination: "/data/:id.json"},
	}}
	got := ""
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/42", nil))
	if got != "/data/42.json" {
		t.Fatalf("rewritten path %q, want /data/42.json", got)
	}
}

func TestPre_RewriteSingleLoop(t *testing.T) {
	s := &Set{rewrites: []Rewrite{
		{Pattern: mustCompile(t, "/A"), Destination: "/B"},
		{Pattern: mustCompile(t, "/B"), Destination: "/A"},
	}}
	got := ""
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/A", nil))
	if got != "/B" {
		t.Fatalf("rewritten %q, want /B (rewrite must not re-run)", got)
	}
}

func TestPre_RewriteNoMatchUntouched(t *testing.T) {
	s := &Set{rewrites: []Rewrite{
		{Pattern: mustCompile(t, "/api/:id"), Destination: "/data/:id.json"},
	}}
	got := ""
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/static/x.css", nil))
	if got != "/static/x.css" {
		t.Fatalf("path mutated to %q, want untouched", got)
	}
}

func TestPre_CleanUrlsRewritesToHTML(t *testing.T) {
	s := &Set{cleanUrls: cleanUrlsValue{enabled: true}}
	s.SetExists(func(p string) bool { return p == "/about.html" })
	got := ""
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/about", nil))
	if got != "/about.html" {
		t.Fatalf("path %q, want /about.html", got)
	}
}

func TestPre_CleanUrlsRedirectsAwayFromHTML(t *testing.T) {
	s := &Set{cleanUrls: cleanUrlsValue{enabled: true}}
	s.SetExists(func(p string) bool { return p == "/about.html" })
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next should not be called")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/about.html", nil))
	if rec.Code != 301 {
		t.Fatalf("status %d, want 301", rec.Code)
	}
	if rec.Header().Get("Location") != "/about" {
		t.Fatalf("Location %q", rec.Header().Get("Location"))
	}
}

func TestPre_CleanUrlsDisabled(t *testing.T) {
	s := &Set{cleanUrls: cleanUrlsValue{enabled: false}}
	s.SetExists(func(p string) bool { return p == "/about.html" })
	got := ""
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/about", nil))
	if got != "/about" {
		t.Fatalf("path %q, want /about (cleanUrls disabled)", got)
	}
}

func TestPre_CleanUrlsNoExistsCallbackIsNoOp(t *testing.T) {
	s := &Set{cleanUrls: cleanUrlsValue{enabled: true}}
	got := ""
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/about", nil))
	if got != "/about" {
		t.Fatalf("path %q, want /about (no SetExists)", got)
	}
}

func TestPre_TrailingSlashStrip(t *testing.T) {
	f := false
	s := &Set{trailingSlash: &f}
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next should not be called")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/dir/", nil))
	if rec.Code != 301 {
		t.Fatalf("status %d, want 301", rec.Code)
	}
	if rec.Header().Get("Location") != "/dir" {
		t.Fatalf("Location %q, want /dir", rec.Header().Get("Location"))
	}
}

func TestPre_TrailingSlashAdd(t *testing.T) {
	v := true
	s := &Set{trailingSlash: &v}
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next should not be called")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/dir", nil))
	if rec.Code != 301 {
		t.Fatalf("status %d, want 301", rec.Code)
	}
	if rec.Header().Get("Location") != "/dir/" {
		t.Fatalf("Location %q, want /dir/", rec.Header().Get("Location"))
	}
}

func TestPre_TrailingSlashAddSkipsExtensions(t *testing.T) {
	v := true
	s := &Set{trailingSlash: &v}
	called := false
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/style.css", nil))
	if !called {
		t.Fatal("file with extension should not be redirected to add trailing slash")
	}
}

func TestPre_TrailingSlashUnsetIsNoOp(t *testing.T) {
	s := &Set{}
	called := false
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/dir/", nil))
	if !called {
		t.Fatal("absent trailingSlash should not affect requests")
	}
}

func TestPre_NilSetPassThrough(t *testing.T) {
	var s *Set
	called := false
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/anything", nil))
	if !called {
		t.Fatal("nil Set.Pre() should be a pass-through")
	}
}
