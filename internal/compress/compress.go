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

// Negotiate inspects an Accept-Encoding header value and returns the
// preferred encoding among "br", "gzip", and "" (identity). Preference:
// higher q-value wins; on ties, br > gzip > identity. A "*" token is
// expanded to whichever of br/gzip were not explicitly listed.
func Negotiate(acceptEncoding string) string {
	if acceptEncoding == "" {
		return ""
	}
	brQ, gzipQ, starQ := -1.0, -1.0, -1.0
	for _, part := range strings.Split(acceptEncoding, ",") {
		token, q := parseEncoding(strings.TrimSpace(part))
		switch token {
		case "br":
			if q > brQ {
				brQ = q
			}
		case "gzip":
			if q > gzipQ {
				gzipQ = q
			}
		case "*":
			if q > starQ {
				starQ = q
			}
		}
	}
	// Star fills in any encoding the client didn't mention.
	if brQ < 0 && starQ >= 0 {
		brQ = starQ
	}
	if gzipQ < 0 && starQ >= 0 {
		gzipQ = starQ
	}
	switch {
	case brQ > gzipQ && brQ > 0:
		return "br"
	case gzipQ > brQ && gzipQ > 0:
		return "gzip"
	case brQ > 0: // tie → br wins
		return "br"
	case gzipQ > 0:
		return "gzip"
	default:
		return ""
	}
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
