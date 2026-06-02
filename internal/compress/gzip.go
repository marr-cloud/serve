package compress

import (
	"compress/gzip"
	"io"
)

// NewGzipEncoder wraps w with a gzip writer at the default compression level.
// Close MUST be called to flush the gzip trailer.
func NewGzipEncoder(w io.Writer) Encoder {
	return gzip.NewWriter(w)
}
