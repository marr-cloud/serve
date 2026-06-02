//go:build windows

package listener

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"

	winio "github.com/Microsoft/go-winio"
)

func TestBuildPipe_BindAndDial(t *testing.T) {
	addr := fmt.Sprintf(`\\.\pipe\serve-test-%d`, os.Getpid())
	ln, err := buildPipe(addr)
	if err != nil {
		t.Fatalf("buildPipe: %v", err)
	}
	defer ln.Close()

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("pipe-ok"))
	})}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	conn, err := winio.DialPipe(addr, nil)
	if err != nil {
		t.Fatalf("DialPipe: %v", err)
	}
	defer conn.Close()
	if _, err := fmt.Fprint(conn, "GET / HTTP/1.0\r\n\r\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	resp, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytesContains(resp, []byte("pipe-ok")) {
		t.Fatalf("expected pipe-ok in response, got:\n%s", resp)
	}
}

func bytesContains(haystack, needle []byte) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == string(needle) {
			return true
		}
	}
	return false
}
