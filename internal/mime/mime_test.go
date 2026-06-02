package mime

import "testing"

func TestTypeByExtension(t *testing.T) {
	tests := []struct {
		ext  string
		want string
	}{
		{".html", "text/html; charset=utf-8"},
		{".HTML", "text/html; charset=utf-8"},
		{".css", "text/css; charset=utf-8"},
		{".js", "application/javascript; charset=utf-8"},
		{".json", "application/json; charset=utf-8"},
		{".png", "image/png"},
		{".jpg", "image/jpeg"},
		{".svg", "image/svg+xml"},
		{".woff2", "font/woff2"},
		{".wasm", "application/wasm"},
		{".map", "application/json; charset=utf-8"},
		{".unknown_xyz", ""},
	}
	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			got := TypeByExtension(tt.ext)
			if got != tt.want {
				t.Fatalf("ext %q: got %q, want %q", tt.ext, got, tt.want)
			}
		})
	}
}

func TestIsAlreadyCompressed(t *testing.T) {
	cases := map[string]bool{
		".jpg":   true,
		".png":   true,
		".webp":  true,
		".gz":    true,
		".br":    true,
		".woff2": true,
		".zip":   true,
		".mp4":   true,
		".html":  false,
		".js":    false,
		".css":   false,
		".json":  false,
		"":       false,
	}
	for ext, want := range cases {
		if got := IsAlreadyCompressed(ext); got != want {
			t.Errorf("ext %q: got %v, want %v", ext, got, want)
		}
	}
}
