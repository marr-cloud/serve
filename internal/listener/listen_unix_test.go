//go:build unix

package listener

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildUnix_BindsAndCleansUp(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "test.sock")
	ln, err := buildUnix(sock)
	if err != nil {
		t.Fatalf("buildUnix: %v", err)
	}
	fi, err := os.Stat(sock)
	if err != nil {
		t.Fatalf("socket file missing after bind: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0o660 {
		t.Fatalf("socket mode %o, want 0660", got)
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.Close()
	if err := ln.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Fatalf("socket file still present after Close: err=%v", err)
	}
}

func TestBuildUnix_RemovesStaleSocketFile(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "stale.sock")
	if err := os.WriteFile(sock, []byte{}, 0o600); err != nil {
		t.Fatalf("seed stale file: %v", err)
	}
	ln, err := buildUnix(sock)
	if err != nil {
		t.Fatalf("buildUnix: %v", err)
	}
	_ = ln.Close()
}
