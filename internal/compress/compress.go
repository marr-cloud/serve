// Package compress negotiates and applies HTTP content encoding.
// F1 supports gzip and identity only; F3 adds brotli.
package compress

import (
	"io"
	"strconv"
	"strings"
)

// Encoder wraps an io.Writer to apply content encoding.
type Encoder interface {
	io.WriteCloser
}

// Negotiate inspects an Accept-Encoding header and returns the chosen encoding.
// Returns "" (identity) when no supported encoding is acceptable.
func Negotiate(acceptEncoding string) string {
	if acceptEncoding == "" {
		return ""
	}
	gzipWeight, starWeight := -1.0, -1.0
	for _, part := range strings.Split(acceptEncoding, ",") {
		token, q := parseEncoding(strings.TrimSpace(part))
		switch token {
		case "gzip":
			gzipWeight = q
		case "*":
			starWeight = q
		}
	}
	if gzipWeight > 0 {
		return "gzip"
	}
	if gzipWeight < 0 && starWeight > 0 {
		return "gzip"
	}
	return ""
}

func parseEncoding(s string) (string, float64) {
	semi := strings.IndexByte(s, ';')
	if semi < 0 {
		return strings.ToLower(s), 1.0
	}
	token := strings.ToLower(strings.TrimSpace(s[:semi]))
	rest := s[semi+1:]
	if idx := strings.Index(rest, "q="); idx >= 0 {
		val := rest[idx+2:]
		if end := strings.IndexByte(val, ';'); end >= 0 {
			val = val[:end]
		}
		if v, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil {
			return token, v
		}
	}
	return token, 1.0
}
