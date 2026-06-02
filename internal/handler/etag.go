package handler

import (
	"fmt"
	"time"
)

// etagInfo is the minimal slice of fs.FileInfo that generateETag needs.
// Defined as an interface so tests can supply fakes without a full FileInfo.
type etagInfo interface {
	Size() int64
	ModTime() time.Time
}

// generateETag returns a strong ETag of the form `"<modtime-nanos>-<size>"`.
// Both components are hex. Deterministic given (modtime, size).
func generateETag(info etagInfo) string {
	return fmt.Sprintf(`"%x-%x"`, info.ModTime().UnixNano(), info.Size())
}
