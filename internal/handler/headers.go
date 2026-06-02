package handler

import (
	"net/http"
	"path/filepath"

	"github.com/marr-cloud/serve/internal/mime"
)

// contentTypeFor returns the Content-Type for a file path. If the extension
// is unknown, sniff is called for fallback. Pass nil to skip sniffing and
// return "application/octet-stream".
func contentTypeFor(path string, sniff func(string) string) string {
	if ct := mime.TypeByExtension(filepath.Ext(path)); ct != "" {
		return ct
	}
	if sniff != nil {
		return sniff(path)
	}
	return "application/octet-stream"
}

// corsMiddleware optionally adds CORS headers. When enabled, OPTIONS
// requests return 204 with the headers set and short-circuit downstream
// handlers.
func corsMiddleware(enabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if !enabled {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "*")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
