package logx

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiddleware_LogsAfterServe(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	h := Middleware(logger, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("hello"))
	}))

	req := httptest.NewRequest("GET", "/x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	out := buf.String()
	if !strings.Contains(out, "GET /x 418 5") {
		t.Fatalf("unexpected log line: %q", out)
	}
}

func TestMiddleware_Disabled(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	h := Middleware(logger, true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if buf.Len() != 0 {
		t.Fatalf("expected no logs when disabled, got %q", buf.String())
	}
}
