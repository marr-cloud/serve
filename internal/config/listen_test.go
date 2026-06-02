package config

import (
	"strings"
	"testing"
)

func TestParseListenURI(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{"bare port", "3000", "0.0.0.0:3000", ""},
		{"colon port", ":3000", "0.0.0.0:3000", ""},
		{"host and port", "localhost:3000", "localhost:3000", ""},
		{"tcp scheme", "tcp://localhost:3000", "localhost:3000", ""},
		{"ipv4 and port", "127.0.0.1:3000", "127.0.0.1:3000", ""},
		{"ipv6 and port", "[::1]:3000", "[::1]:3000", ""},
		{"port zero allowed", "0", "0.0.0.0:0", ""},
		{"empty", "", "", "empty"},
		{"non-numeric port", "host:abc", "", "invalid port"},
		{"port out of range high", "host:99999", "", "invalid port"},
		{"port negative", "host:-1", "", "invalid port"},
		{"unsupported scheme unix", "unix:/tmp/s.sock", "", "not supported"},
		{"unsupported scheme pipe", "pipe:\\\\.\\pipe\\s", "", "not supported"},
		{"unsupported scheme unix with //", "unix:///tmp/s.sock", "", "not supported"},
		{"unsupported scheme pipe with //", "pipe:////.//pipe//s", "", "not supported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseListenURI(tt.input)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
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
