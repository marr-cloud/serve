// Package mime maps file extensions to MIME types deterministically,
// without depending on /etc/mime.types or the Windows registry.
package mime

import (
	stdhttp "net/http"
	"os"
	"strings"
)

var table = map[string]string{
	".html":  "text/html; charset=utf-8",
	".htm":   "text/html; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".js":    "application/javascript; charset=utf-8",
	".mjs":   "application/javascript; charset=utf-8",
	".json":  "application/json; charset=utf-8",
	".map":   "application/json; charset=utf-8",
	".xml":   "application/xml; charset=utf-8",
	".txt":   "text/plain; charset=utf-8",
	".md":    "text/markdown; charset=utf-8",
	".csv":   "text/csv; charset=utf-8",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".gif":   "image/gif",
	".webp":  "image/webp",
	".svg":   "image/svg+xml",
	".ico":   "image/x-icon",
	".pdf":   "application/pdf",
	".zip":   "application/zip",
	".gz":    "application/gzip",
	".br":    "application/octet-stream",
	".tar":   "application/x-tar",
	".wasm":  "application/wasm",
	".mp3":   "audio/mpeg",
	".mp4":   "video/mp4",
	".webm":  "video/webm",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
	".otf":   "font/otf",
}

// TypeByExtension returns the MIME type for ext (case-insensitive, leading dot
// optional). Empty string means "unknown — use sniffer".
func TypeByExtension(ext string) string {
	if ext == "" {
		return ""
	}
	if ext[0] != '.' {
		ext = "." + ext
	}
	return table[strings.ToLower(ext)]
}

var compressedExts = map[string]struct{}{
	".jpg": {}, ".jpeg": {}, ".png": {}, ".gif": {}, ".webp": {},
	".gz": {}, ".br": {}, ".zip": {}, ".tar": {},
	".mp3": {}, ".mp4": {}, ".webm": {},
	".woff": {}, ".woff2": {},
}

// IsAlreadyCompressed reports whether content with this extension is typically
// already compressed (and therefore not worth re-compressing).
func IsAlreadyCompressed(ext string) bool {
	if ext == "" {
		return false
	}
	if ext[0] != '.' {
		ext = "." + ext
	}
	_, ok := compressedExts[strings.ToLower(ext)]
	return ok
}

// SniffContentType reads up to 512 bytes from path and uses net/http's
// content type sniffer, adding "; charset=utf-8" for text/ results.
// Returns "application/octet-stream" as ultimate fallback.
func SniffContentType(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n == 0 {
		return "application/octet-stream"
	}
	ct := stdhttp.DetectContentType(buf[:n])
	if strings.HasPrefix(ct, "text/") && !strings.Contains(ct, "charset") {
		ct += "; charset=utf-8"
	}
	return ct
}
