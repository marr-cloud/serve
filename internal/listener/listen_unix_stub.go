//go:build !unix

package listener

import (
	"fmt"
	"net"
)

func buildUnix(addr string) (net.Listener, error) {
	return nil, fmt.Errorf("unix sockets require Linux/macOS")
}
