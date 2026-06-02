package handler

import (
	"net/http"
	"runtime"
	"strings"
	"testing"
)

func sep() string {
	if runtime.GOOS == "windows" {
		return `\`
	}
	return "/"
}

func TestResolvePath(t *testing.T) {
	root := "/var/www"
	if runtime.GOOS == "windows" {
		root = `C:\www`
	}

	tests := []struct {
		name    string
		urlPath string
		want    string
		wantErr string
	}{
		{"root", "/", root, ""},
		{"file at root", "/index.html", root + sep() + "index.html", ""},
		{"nested", "/a/b/c.txt", root + sep() + "a" + sep() + "b" + sep() + "c.txt", ""},
		{"traversal dotdot", "/../etc/passwd", "", "traversal"},
		{"double slash", "//a", root + sep() + "a", ""},
		{"trailing slash kept", "/dir/", root + sep() + "dir", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolvePath(root, tt.urlPath)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func mkReq(method, path, accept string) *http.Request {
	r, _ := http.NewRequest(method, path, nil)
	if accept != "" {
		r.Header.Set("Accept", accept)
	}
	return r
}

func TestShouldServeSPA(t *testing.T) {
	cases := []struct {
		name string
		req  *http.Request
		want bool
	}{
		{"GET html accept", mkReq("GET", "/route", "text/html,*/*"), true},
		{"HEAD html accept", mkReq("HEAD", "/route", "text/html"), true},
		{"POST not allowed", mkReq("POST", "/route", "text/html"), false},
		{"JSON accept not allowed", mkReq("GET", "/api/x", "application/json"), false},
		{"asset extension blocks", mkReq("GET", "/app.js", "text/html"), false},
		{"png asset blocks", mkReq("GET", "/logo.png", "text/html"), false},
		{"no accept header", mkReq("GET", "/route", ""), false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldServeSPA(tt.req); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
