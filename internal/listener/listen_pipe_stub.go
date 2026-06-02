//go:build !windows

package listener

import (
	"fmt"
	"net"
)

func buildPipe(addr string) (net.Listener, error) {
	return nil, fmt.Errorf("named pipes require Windows")
}
