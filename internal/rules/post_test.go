package rules

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPost_AddsHeader(t *testing.T) {
	s := &Set{headers: []HeaderRule{{
		Pattern: mustCompile(t, "/*.css"),
		Headers: []HeaderKV{{Key: "Cache-Control", Value: "max-age=60"}},
	}}}
	h := s.Post()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/style.css", nil))
	if got := rec.Header().Get("Cache-Control"); got != "max-age=60" {
		t.Fatalf("Cache-Control %q", got)
	}
}

func TestPost_NoMatchNoChange(t *testing.T) {
	s := &Set{headers: []HeaderRule{{
		Pattern: mustCompile(t, "/*.css"),
		Headers: []HeaderKV{{Key: "Cache-Control", Value: "max-age=60"}},
	}}}
	h := s.Post()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/style.js", nil))
	if rec.Header().Get("Cache-Control") != "" {
		t.Fatalf("unexpected Cache-Control on non-match")
	}
}

func TestPost_MultipleMatchingRulesAllApply(t *testing.T) {
	s := &Set{headers: []HeaderRule{
		{Pattern: mustCompile(t, "/**"), Headers: []HeaderKV{{Key: "X-One", Value: "a"}}},
		{Pattern: mustCompile(t, "/x"), Headers: []HeaderKV{{Key: "X-Two", Value: "b"}}},
	}}
	h := s.Post()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Header().Get("X-One") != "a" || rec.Header().Get("X-Two") != "b" {
		t.Fatalf("X-One=%q X-Two=%q", rec.Header().Get("X-One"), rec.Header().Get("X-Two"))
	}
}

func TestPost_ValueExpansionWithCapture(t *testing.T) {
	s := &Set{headers: []HeaderRule{{
		Pattern: mustCompile(t, "/u/:id"),
		Headers: []HeaderKV{{Key: "X-User", Value: ":id"}},
	}}}
	h := s.Post()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/u/42", nil))
	if rec.Header().Get("X-User") != "42" {
		t.Fatalf("X-User %q, want 42", rec.Header().Get("X-User"))
	}
}

func TestPost_OverridesHandlerHeader(t *testing.T) {
	s := &Set{headers: []HeaderRule{{
		Pattern: mustCompile(t, "/**"),
		Headers: []HeaderKV{{Key: "Content-Type", Value: "text/x-custom"}},
	}}}
	h := s.Post()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(200)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if got := rec.Header().Get("Content-Type"); got != "text/x-custom" {
		t.Fatalf("Content-Type %q, rule should win over handler", got)
	}
}

func TestPost_NilSetPassThrough(t *testing.T) {
	var s *Set
	called := false
	h := s.Post()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))
	if !called {
		t.Fatal("nil Set.Post() should pass through")
	}
}
