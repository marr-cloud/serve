package listener

import (
	"fmt"
	"net"
)

// buildTCP binds canon (a `host:port` string already validated by
// config.ParseListenURI). On EADDRINUSE, when allowPortSwitch is true,
// probes the next 100 ports via NextAvailable.
func buildTCP(canon string, allowPortSwitch bool) (net.Listener, error) {
	l, err := net.Listen("tcp", canon)
	if err == nil {
		return l, nil
	}
	if !allowPortSwitch {
		return nil, fmt.Errorf("listen %q: %w", canon, err)
	}
	next, switchErr := NextAvailable(canon)
	if switchErr != nil {
		return nil, fmt.Errorf("port switch from %q: %w", canon, switchErr)
	}
	l, err = net.Listen("tcp", next)
	if err != nil {
		return nil, fmt.Errorf("listen %q after switch: %w", next, err)
	}
	return l, nil
}
