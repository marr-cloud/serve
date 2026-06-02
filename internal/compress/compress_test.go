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
		{"br only", "br", "br"},
		{"br preferred over gzip", "br, gzip", "br"},
		{"q-value beats preference", "br;q=0.5, gzip;q=1.0", "gzip"},
		{"both at same low q -> br", "br;q=0.3, gzip;q=0.3", "br"},
		{"deflate only", "deflate", ""},
		{"identity returns empty", "identity", ""},
		{"star prefers br", "*", "br"},
		{"gzip explicitly rejected", "gzip;q=0", ""},
		{"gzip explicitly rejected but br accepted", "br, gzip;q=0", "br"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Negotiate(tt.acceptHeader); got != tt.want {
				t.Fatalf("Accept-Encoding %q: got %q, want %q", tt.acceptHeader, got, tt.want)
			}
		})
	}
}
