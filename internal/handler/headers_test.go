package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestContentTypeFor(t *testing.T) {
	cases := map[string]string{
		"index.html": "text/html; charset=utf-8",
		"app.js":     "application/javascript; charset=utf-8",
		"data.json":  "application/json; charset=utf-8",
		"logo.PNG":   "image/png",
		"font.woff2": "font/woff2",
	}
	for path, want := range cases {
		t.Run(path, func(t *testing.T) {
			if got := contentTypeFor(path, nil); got != want {
				t.Fatalf("path %q: got %q, want %q", path, got, want)
			}
		})
	}
}

func TestCORSMiddleware(t *testing.T) {
	handler := corsMiddleware(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS: status %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("OPTIONS: ACAO %q, want *", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: status %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("GET: ACAO %q, want *", got)
	}
}

func TestCORSDisabled(t *testing.T) {
	handler := corsMiddleware(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("CORS off: ACAO %q, want empty", got)
	}
}
