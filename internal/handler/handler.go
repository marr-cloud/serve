// Package handler is the HTTP handler that backs the serve CLI.
package handler

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"

	"serve/internal/compress"
	"serve/internal/config"
	"serve/internal/mime"
)

// New returns an http.Handler that serves files from fsys according to cfg.
func New(cfg config.Config, fsys fs.FS) http.Handler {
	core := &core{cfg: cfg, fsys: fsys}
	var h http.Handler = http.HandlerFunc(core.serve)
	h = corsMiddleware(cfg.CORS)(h)
	return h
}

type core struct {
	cfg  config.Config
	fsys fs.FS
}

func (c *core) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	urlPath := r.URL.Path
	if urlPath == "" {
		urlPath = "/"
	}
	// Reject `..` segments on the raw URL before normalization. [BUG#4]
	// filepath.Clean would collapse "/../etc/passwd" to "/etc/passwd",
	// hiding the original intent — so check segments first.
	for _, seg := range strings.Split(strings.TrimPrefix(urlPath, "/"), "/") {
		if seg == ".." {
			http.NotFound(w, r)
			return
		}
	}
	cleaned := strings.TrimPrefix(filepath.ToSlash(filepath.Clean("/"+strings.TrimPrefix(urlPath, "/"))), "/")
	if cleaned == "" {
		cleaned = "."
	}

	info, err := fs.Stat(c.fsys, cleaned)
	if err != nil {
		if c.cfg.Single && shouldServeSPA(r) {
			c.serveFile(w, r, "index.html")
			return
		}
		http.NotFound(w, r)
		return
	}

	// [BUG#7] symlink consistency: if symlinks are disabled and this entry is a
	// symlink, 404 before we open it via the FS (which would follow it).
	if !c.cfg.Symlinks {
		if lstat, ok := c.fsys.(interface {
			Lstat(string) (fs.FileInfo, error)
		}); ok {
			if li, lerr := lstat.Lstat(cleaned); lerr == nil && li.Mode()&fs.ModeSymlink != 0 {
				http.NotFound(w, r)
				return
			}
		}
	}

	if info.IsDir() {
		// Redirect /dir to /dir/ so relative links inside the listing or
		// inside any index.html resolve against the correct base URL.
		if !strings.HasSuffix(urlPath, "/") {
			http.Redirect(w, r, urlPath+"/", http.StatusMovedPermanently)
			return
		}
		indexPath := pathJoin(cleaned, "index.html")
		if idx, err := fs.Stat(c.fsys, indexPath); err == nil && !idx.IsDir() {
			c.serveFile(w, r, indexPath)
			return
		}
		if err := serveDirectory(w, r, c.fsys, cleaned, urlPath); err != nil {
			http.Error(w, "Unable to read directory", http.StatusInternalServerError)
		}
		return
	}

	c.serveFile(w, r, cleaned)
}

func pathJoin(a, b string) string {
	if a == "." || a == "" {
		return b
	}
	return a + "/" + b
}

// matchesETag implements RFC 7232 multi-value If-None-Match comparison.
// "*" matches any ETag; otherwise the header is a comma-separated list of
// quoted ETags and we look for an exact match.
func matchesETag(ifNoneMatch, etag string) bool {
	if ifNoneMatch == "" {
		return false
	}
	if ifNoneMatch == "*" {
		return true
	}
	for _, v := range strings.Split(ifNoneMatch, ",") {
		if strings.TrimSpace(v) == etag {
			return true
		}
	}
	return false
}

// serveFile is the single point where ETag, Content-Type, compression, and
// Range coexist. Gzip and Range are mutually exclusive: when both could apply,
// Range wins (compression is skipped). [BUG#1]
func (c *core) serveFile(w http.ResponseWriter, r *http.Request, fsPath string) {
	info, err := fs.Stat(c.fsys, fsPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	contentType := contentTypeFor(fsPath, func(string) string {
		return sniffViaFS(c.fsys, fsPath)
	})
	w.Header().Set("Content-Type", contentType)

	if c.cfg.NoETag {
		w.Header().Set("Last-Modified", info.ModTime().UTC().Format(http.TimeFormat))
	} else {
		etag := generateETag(etagAdapter{info})
		w.Header().Set("ETag", etag)
		if matchesETag(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	wantGzip := !c.cfg.NoCompression &&
		r.Header.Get("Range") == "" && // [BUG#1] never compress when Range is requested
		compress.Negotiate(r.Header.Get("Accept-Encoding")) == "gzip" &&
		isCompressible(contentType, fsPath, info.Size())

	if wantGzip {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
		// [BUG#2] Content-Length is unknown post-compression; let Go use chunked TE.
		w.Header().Del("Content-Length")

		if r.Method == http.MethodHead {
			return
		}
		f, err := c.fsys.Open(fsPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer f.Close()
		gz := compress.NewGzipEncoder(w)
		defer gz.Close()
		_, _ = io.Copy(gz, f)
		return
	}

	f, err := c.fsys.Open(fsPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	var rs io.ReadSeeker
	if seeker, ok := f.(io.ReadSeeker); ok {
		rs = seeker
	} else {
		buf, ferr := io.ReadAll(f)
		if ferr != nil {
			http.Error(w, ferr.Error(), http.StatusInternalServerError)
			return
		}
		rs = bytes.NewReader(buf)
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), rs)
}

type etagAdapter struct{ fs.FileInfo }

func sniffViaFS(fsys fs.FS, p string) string {
	f, err := fsys.Open(p)
	if err != nil {
		return "application/octet-stream"
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n == 0 {
		return "application/octet-stream"
	}
	ct := http.DetectContentType(buf[:n])
	if strings.HasPrefix(ct, "text/") && !strings.Contains(ct, "charset") {
		ct += "; charset=utf-8"
	}
	return ct
}

const minCompressBytes = 1024

func isCompressible(contentType, path string, size int64) bool {
	if size < minCompressBytes {
		return false
	}
	if mime.IsAlreadyCompressed(filepath.Ext(path)) {
		return false
	}
	prefixes := []string{
		"text/",
		"application/javascript",
		"application/json",
		"application/xml",
		"application/xhtml+xml",
		"application/wasm",
		"image/svg+xml",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(contentType, p) {
			return true
		}
	}
	return false
}
