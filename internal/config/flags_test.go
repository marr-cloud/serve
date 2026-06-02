package config

import (
	"reflect"
	"testing"
)

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want Config
	}{
		{
			name: "defaults",
			args: []string{"serve"},
			want: Config{},
		},
		{
			name: "directory positional",
			args: []string{"serve", "./public"},
			want: Config{Directory: "./public"},
		},
		{
			name: "short single",
			args: []string{"serve", "-s"},
			want: Config{Single: true},
		},
		{
			name: "long single",
			args: []string{"serve", "--single"},
			want: Config{Single: true},
		},
		{
			name: "port short",
			args: []string{"serve", "-p", "8080"},
			want: Config{Port: 8080},
		},
		{
			name: "multiple listen",
			args: []string{"serve", "-l", "3000", "-l", "tcp://0.0.0.0:4000"},
			want: Config{Listen: []string{"3000", "tcp://0.0.0.0:4000"}},
		},
		{
			name: "cors and no-clipboard",
			args: []string{"serve", "-C", "-n"},
			want: Config{CORS: true, NoClipboard: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := ParseFlags(tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseFlags_SSLFlags(t *testing.T) {
	cfg, set, err := ParseFlags([]string{
		"serve",
		"--ssl-cert", "/etc/cert.pem",
		"--ssl-key", "/etc/key.pem",
		"--ssl-pass", "/etc/pass.txt",
	})
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if cfg.SSLCert != "/etc/cert.pem" || cfg.SSLKey != "/etc/key.pem" || cfg.SSLPass != "/etc/pass.txt" {
		t.Fatalf("cfg=%+v", cfg)
	}
	for _, name := range []string{"ssl-cert", "ssl-key", "ssl-pass"} {
		if !set[name] {
			t.Fatalf("expected %s in cliSet", name)
		}
	}
}

func TestParseFlags_ReturnsCLISet(t *testing.T) {
	cfg, set, err := ParseFlags([]string{"serve", "-p", "8080", "-s", "."})
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if cfg.Port != 8080 {
		t.Fatalf("Port %d, want 8080", cfg.Port)
	}
	if !set["p"] {
		t.Fatalf("expected 'p' in cliSet, got %v", set)
	}
	if !set["s"] {
		t.Fatalf("expected 's' in cliSet, got %v", set)
	}
	if set["S"] {
		t.Fatalf("did not pass -S; should not be in cliSet")
	}
}
