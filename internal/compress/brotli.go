package compress

import (
	"io"

	"github.com/andybalholm/brotli"
)

// NewBrotliEncoder returns an Encoder writing brotli-compressed bytes to w.
// Quality 5 is the default: best ratio/CPU trade-off for static assets per
// the upstream benchmarks (close to gzip 9 ratio at roughly gzip 6 cost).
func NewBrotliEncoder(w io.Writer) Encoder {
	return brotli.NewWriterLevel(w, 5)
}
