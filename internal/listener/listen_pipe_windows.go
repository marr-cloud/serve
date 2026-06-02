//go:build windows

package listener

import (
	"net"

	winio "github.com/Microsoft/go-winio"
)

// buildPipe binds a Windows named pipe at addr (e.g. `\\.\pipe\serve`).
// The pipe is created with a permissive SDDL granting all access to
// "Everyone" — appropriate for a local static file server.
func buildPipe(addr string) (net.Listener, error) {
	cfg := &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;WD)",
	}
	return winio.ListenPipe(addr, cfg)
}
