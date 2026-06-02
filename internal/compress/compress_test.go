package compress

import "testing"

func TestNegotiate(t *testing.T) {
	tests := []struct {
		name         string
		acceptHeader string
		want         string
	}{
		{"empty", "", ""},
		{"gzip only", "gzip", "gzip"},
		{"gzip with quality", "gzip;q=1.0", "gzip"},
		{"gzip and br", "br, gzip", "gzip"},
		{"deflate only", "deflate", ""},
		{"star", "*", "gzip"},
		{"gzip explicitly rejected", "gzip;q=0", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Negotiate(tt.acceptHeader); got != tt.want {
				t.Fatalf("Accept-Encoding %q: got %q, want %q", tt.acceptHeader, got, tt.want)
			}
		})
	}
}
