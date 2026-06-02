// Package logx provides a tiny request logger middleware.
package logx

import (
	"log"
	"net/http"
	"time"
)

// Middleware logs `METHOD PATH STATUS BYTES DURATION` after each request.
// When disabled is true, the returned middleware is a passthrough.
func Middleware(logger *log.Logger, disabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if disabled {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &recorder{ResponseWriter: w, status: 200}
			next.ServeHTTP(rec, r)
			logger.Printf("%s %s %d %d %s",
				r.Method, r.URL.Path, rec.status, rec.bytes, time.Since(start))
		})
	}
}

type recorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *recorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}
