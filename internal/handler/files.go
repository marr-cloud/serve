package handler

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
)

// resolvePath maps a URL path to an on-disk path under root, rejecting any
// request that would escape root via "..".
func resolvePath(root, urlPath string) (string, error) {
	if i := strings.IndexAny(urlPath, "?#"); i >= 0 {
		urlPath = urlPath[:i]
	}
	// Reject `..` segments in the raw URL before any normalization.
	// filepath.Clean would collapse "/../etc/passwd" to "/etc/passwd",
	// hiding the original intent from the post-Clean Rel check.
	for _, seg := range strings.Split(strings.TrimPrefix(urlPath, "/"), "/") {
		if seg == ".." {
			return "", errors.New("path traversal rejected")
		}
	}
	clean := filepath.Clean("/" + strings.TrimPrefix(urlPath, "/"))
	full := filepath.Join(root, filepath.FromSlash(clean))
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("abs(root): %w", err)
	}
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", fmt.Errorf("abs(full): %w", err)
	}
	rel, err := filepath.Rel(absRoot, absFull)
	if err != nil {
		return "", fmt.Errorf("rel: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path traversal rejected")
	}
	return full, nil
}

// assetExtensions are file types we never serve through the SPA fallback.
// A request for one of these is treated as a missing asset, not a route.
var assetExtensions = map[string]struct{}{
	".js": {}, ".mjs": {}, ".css": {}, ".json": {}, ".map": {},
	".png": {}, ".jpg": {}, ".jpeg": {}, ".gif": {}, ".svg": {}, ".ico": {},
	".webp": {}, ".mp4": {}, ".mp3": {}, ".webm": {},
	".woff": {}, ".woff2": {}, ".ttf": {}, ".otf": {},
	".pdf": {}, ".zip": {}, ".gz": {}, ".tar": {}, ".wasm": {},
	".xml": {}, ".txt": {}, ".csv": {}, ".md": {},
}

// shouldServeSPA reports whether a missing file should be replaced with
// index.html. True iff method is GET/HEAD, Accept includes text/html, and
// the URL path doesn't have a known asset extension.
func shouldServeSPA(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if !strings.Contains(r.Header.Get("Accept"), "text/html") {
		return false
	}
	ext := strings.ToLower(filepath.Ext(r.URL.Path))
	if _, isAsset := assetExtensions[ext]; isAsset {
		return false
	}
	return true
}
