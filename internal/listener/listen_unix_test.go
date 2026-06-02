//go:build unix

package listener

import (
	"fmt"
	"net"
	"os"
	"testing"
)

// shortSockPath returns a path under /tmp that respects macOS's
// 104-byte sun_path limit. t.TempDir() on Darwin produces paths under
// /var/folders/... that routinely exceed that limit.
func shortSockPath(t *testing.T, name string) string {
	t.Helper()
	p := fmt.Sprintf("/tmp/serve-%s-%d.sock", name, os.Getpid())
	t.Cleanup(func() { _ = os.Remove(p) })
	return p
}

func TestBuildUnix_BindsAndCleansUp(t *testing.T) {
	sock := shortSockPath(t, "bind")
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
	sock := shortSockPath(t, "stale")
	if err := os.WriteFile(sock, []byte{}, 0o600); err != nil {
		t.Fatalf("seed stale file: %v", err)
	}
	ln, err := buildUnix(sock)
	if err != nil {
		t.Fatalf("buildUnix: %v", err)
	}
	_ = ln.Close()
}
