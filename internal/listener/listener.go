// Package listener turns Config.Listen strings into net.Listener instances,
// optionally trying a successor port when the requested one is taken.
package listener

import (
	"fmt"
	"net"
	"time"

	"serve/internal/config"
)

// ShutdownTimeout is how long Server.Shutdown waits for in-flight requests. [BUG#10]
const ShutdownTimeout = 5 * time.Second

// Build resolves each input via config.ParseListenURI and binds it. If
// allowPortSwitch is true, an EADDRINUSE on bind triggers a search for the
// next free port.
func Build(listenAddrs []string, allowPortSwitch bool) ([]net.Listener, error) {
	out := make([]net.Listener, 0, len(listenAddrs))
	for _, a := range listenAddrs {
		canon, err := config.ParseListenURI(a)
		if err != nil {
			closeAll(out)
			return nil, fmt.Errorf("parse %q: %w", a, err)
		}
		l, err := net.Listen("tcp", canon)
		if err != nil {
			if !allowPortSwitch {
				closeAll(out)
				return nil, fmt.Errorf("listen %q: %w", canon, err)
			}
			next, switchErr := NextAvailable(canon)
			if switchErr != nil {
				closeAll(out)
				return nil, fmt.Errorf("port switch from %q: %w", canon, switchErr)
			}
			l, err = net.Listen("tcp", next)
			if err != nil {
				closeAll(out)
				return nil, fmt.Errorf("listen %q after switch: %w", next, err)
			}
		}
		out = append(out, l)
	}
	return out, nil
}

func closeAll(lns []net.Listener) {
	for _, l := range lns {
		_ = l.Close()
	}
}
