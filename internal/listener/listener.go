// Package listener turns Config.Listen strings into net.Listener instances.
// Build dispatches by scheme (tcp/unix/pipe). When tlsCfg is non-nil, the
// returned TCP/unix listeners are wrapped via tls.NewListener; pipe
// listeners are not wrapped (local transport — TLS adds no value).
package listener

import (
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"serve/internal/config"
)

// ShutdownTimeout is how long Server.Shutdown waits for in-flight requests. [BUG#10]
const ShutdownTimeout = 5 * time.Second

// Build resolves each input via config.ParseListenURIScheme and binds it.
// If allowPortSwitch is true, an EADDRINUSE on TCP bind triggers a search
// for the next free port. When tlsCfg is non-nil, TCP and unix listeners
// are wrapped via tls.NewListener; pipe listeners are not wrapped.
func Build(addrs []string, allowPortSwitch bool, tlsCfg *tls.Config) ([]net.Listener, error) {
	out := make([]net.Listener, 0, len(addrs))
	for _, a := range addrs {
		scheme, addr, err := config.ParseListenURIScheme(a)
		if err != nil {
			closeAll(out)
			return nil, fmt.Errorf("parse %q: %w", a, err)
		}
		var ln net.Listener
		switch scheme {
		case "tcp":
			ln, err = buildTCP(addr, allowPortSwitch)
		case "unix":
			ln, err = buildUnix(addr)
		case "pipe":
			ln, err = buildPipe(addr)
		default:
			err = fmt.Errorf("unsupported scheme %q", scheme)
		}
		if err != nil {
			closeAll(out)
			return nil, fmt.Errorf("listen %q: %w", a, err)
		}
		if tlsCfg != nil && scheme != "pipe" {
			ln = tls.NewListener(ln, tlsCfg)
		}
		out = append(out, ln)
	}
	return out, nil
}

func closeAll(lns []net.Listener) {
	for _, l := range lns {
		_ = l.Close()
	}
}
