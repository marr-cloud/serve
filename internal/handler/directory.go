package handler

import (
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"

	"serve/internal/rules"
)

// serveDirectory writes an HTML listing of entries in dirPath (relative to fsys)
// to w. urlPath is the request URL used to build links.
// set may be nil; when non-nil, entries matching IsHidden are filtered out.
func serveDirectory(w http.ResponseWriter, _ *http.Request, fsys fs.FS, dirPath, urlPath string, set *rules.Set) error {
	entries, err := fs.ReadDir(fsys, dirPath)
	if err != nil {
		return err
	}

	if set != nil {
		filtered := entries[:0]
		for _, e := range entries {
			if !set.IsHidden(e.Name()) {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	var b strings.Builder
	fmt.Fprintf(&b, `<!DOCTYPE html><html><head><title>Index of %s</title>
<style>body{font-family:system-ui,sans-serif;margin:2rem;}
h1{border-bottom:1px solid #ccc;padding-bottom:.5rem;}
table{border-collapse:collapse;width:100%%;}
td{padding:.25rem .75rem;border-bottom:1px solid #eee;}
a{color:#06c;text-decoration:none;}a:hover{text-decoration:underline;}
.size,.mtime{color:#666;text-align:right;font-variant-numeric:tabular-nums;}</style>
</head><body><h1>Index of %s</h1><table>`,
		html.EscapeString(urlPath), html.EscapeString(urlPath))

	if urlPath != "/" {
		fmt.Fprint(&b, `<tr><td><a href="../">../</a></td><td></td><td></td></tr>`)
	}

	for _, e := range entries {
		name := e.Name()
		display := html.EscapeString(name)
		// PathEscape encodes spaces, '?', '#', etc. so the link survives
		// the URL parser. html.EscapeString below handles the HTML context.
		href := path.Join(urlPath, url.PathEscape(name))
		if e.IsDir() {
			display += "/"
			if !strings.HasSuffix(href, "/") {
				href += "/"
			}
		}
		info, infoErr := e.Info()
		size, mtime := "", ""
		if infoErr == nil {
			if !e.IsDir() {
				size = formatSize(info.Size())
			}
			mtime = info.ModTime().UTC().Format("2006-01-02 15:04")
		}
		fmt.Fprintf(&b, `<tr><td><a href="%s">%s</a></td><td class="size">%s</td><td class="mtime">%s</td></tr>`,
			html.EscapeString(href), display, size, mtime)
	}

	fmt.Fprint(&b, `</table></body></html>`)
	_, err = w.Write([]byte(b.String()))
	return err
}

func formatSize(n int64) string {
	const k = 1024
	switch {
	case n < k:
		return fmt.Sprintf("%d B", n)
	case n < k*k:
		return fmt.Sprintf("%.1f KiB", float64(n)/k)
	case n < k*k*k:
		return fmt.Sprintf("%.1f MiB", float64(n)/(k*k))
	default:
		return fmt.Sprintf("%.1f GiB", float64(n)/(k*k*k))
	}
}
