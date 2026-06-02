//go:build unix

package listener

import (
	"net"
	"os"
)

// buildUnix binds a unix domain socket at addr. Removes any stale socket file
// at that path first, then creates a fresh one with mode 0660. The returned
// listener removes the socket file on Close so subsequent runs see a clean
// state.
func buildUnix(addr string) (net.Listener, error) {
	_ = os.Remove(addr) // best-effort stale cleanup
	ln, err := net.Listen("unix", addr)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(addr, 0o660); err != nil {
		_ = ln.Close()
		return nil, err
	}
	return &cleanupListener{Listener: ln, path: addr}, nil
}

type cleanupListener struct {
	net.Listener
	path string
}

func (c *cleanupListener) Close() error {
	err := c.Listener.Close()
	_ = os.Remove(c.path)
	return err
}
