package listener

import (
	"net"
	"strings"
	"testing"
)

func TestNextAvailable(t *testing.T) {
	lc, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("setup listen: %v", err)
	}
	defer lc.Close()

	taken := lc.Addr().String()
	got, err := NextAvailable(taken)
	if err != nil {
		t.Fatalf("NextAvailable: %v", err)
	}
	if got == taken {
		t.Fatal("should not return the in-use address")
	}
	if !strings.HasPrefix(got, "127.0.0.1:") {
		t.Fatalf("unexpected host: %q", got)
	}
}
