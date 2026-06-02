package listener

import (
	"fmt"
	"net"
	"strconv"
)

// NextAvailable returns the first host:port after addr that successfully binds.
// Probes up to 100 successor ports; returns an error if none are available.
func NextAvailable(addr string) (string, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("split %q: %w", addr, err)
	}
	base, err := strconv.Atoi(portStr)
	if err != nil {
		return "", fmt.Errorf("port %q: %w", portStr, err)
	}
	for p := base + 1; p < base+1+100 && p < 65536; p++ {
		candidate := net.JoinHostPort(host, strconv.Itoa(p))
		ln, err := net.Listen("tcp", candidate)
		if err == nil {
			_ = ln.Close()
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no free port found near %q", addr)
}
