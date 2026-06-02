// Command serve is a static file server compatible with the npm `serve` CLI.
package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/tiagomelo/go-clipboard/clipboard"

	"serve/internal/config"
	"serve/internal/handler"
	"serve/internal/listener"
	"serve/internal/logx"
	"serve/internal/rules"
)

func main() {
	cfg, cliSet, err := config.ParseFlags(os.Args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if cfg.Help {
		printHelp()
		return
	}
	if cfg.Version {
		fmt.Printf("serve version %s\n", config.Version)
		return
	}

	// Load serve.json (or -c path). cfg.Directory may still be "" here, which
	// is intentional: MergeIntoConfig uses the empty string as the signal that
	// no positional argument was given, and Load falls back to cwd when dir="".
	ruleSet, err := rules.Load(cfg.ConfigFile, cfg.Directory)
	if err != nil {
		log.Fatalf("serve.json: %v", err)
	}
	ruleSet.MergeIntoConfig(&cfg, cliSet)

	// Fallback to cwd only after the merge so serve.json "public" wins over it.
	if cfg.Directory == "" {
		wd, err := os.Getwd()
		if err != nil {
			log.Fatalf("getwd: %v", err)
		}
		cfg.Directory = wd
	}
	if _, err := os.Stat(cfg.Directory); err != nil {
		log.Fatalf("directory %q: %v", cfg.Directory, err)
	}

	fsys := osDirFS(cfg.Directory)

	// Install the existence callback so cleanUrls rules can check the served FS.
	ruleSet.SetExists(func(urlPath string) bool {
		p := strings.TrimPrefix(urlPath, "/")
		if p == "" {
			return false
		}
		_, statErr := fs.Stat(fsys, p)
		return statErr == nil
	})

	if len(cfg.Listen) == 0 {
		if cfg.Port != 0 {
			cfg.Listen = []string{fmt.Sprintf("0.0.0.0:%d", cfg.Port)}
		} else {
			cfg.Listen = []string{"0.0.0.0:3000"}
		}
	}

	tlsCfg, err := listener.LoadTLSConfig(cfg.SSLCert, cfg.SSLKey, cfg.SSLPass)
	if err != nil {
		log.Fatalf("tls: %v", err)
	}

	listeners, err := listener.Build(cfg.Listen, !cfg.NoPortSwitching, tlsCfg)
	if err != nil {
		log.Fatalf("listener: %v", err)
	}

	h := logx.Middleware(log.Default(), cfg.NoRequestLogging)(
		handler.New(cfg, fsys, ruleSet),
	)

	servers := make([]*http.Server, 0, len(listeners))
	for _, l := range listeners {
		srv := &http.Server{Handler: h}
		servers = append(servers, srv)
		go func(s *http.Server, l net.Listener) {
			if err := s.Serve(l); err != nil && err != http.ErrServerClosed {
				log.Printf("serve %s: %v", l.Addr(), err)
			}
		}(srv, l)
	}

	// [BUG#9] Compute the real local address from the first listener (post port-switch).
	scheme := "http"
	if tlsCfg != nil {
		scheme = "https"
	}
	localAddr := ""
	if len(listeners) > 0 {
		if _, port, splitErr := net.SplitHostPort(listeners[0].Addr().String()); splitErr == nil {
			localAddr = scheme + "://localhost:" + port
		}
	}

	printStartupMessage(cfg, listeners, localAddr, scheme)

	if !cfg.NoClipboard && localAddr != "" {
		_ = clipboard.New().CopyText(localAddr)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh
	fmt.Println("\n INFO  Gracefully shutting down. Please wait...")

	ctx, cancel := context.WithTimeout(context.Background(), listener.ShutdownTimeout)
	defer cancel()
	for _, srv := range servers {
		_ = srv.Shutdown(ctx)
	}
}

// lstatFS wraps os.DirFS with Lstat so the handler can detect symlinks
// without following them. [BUG#7]
type lstatFS struct {
	root string
	fs   fs.FS
}

func osDirFS(root string) lstatFS {
	return lstatFS{root: root, fs: os.DirFS(root)}
}

func (l lstatFS) Open(name string) (fs.File, error) { return l.fs.Open(name) }

func (l lstatFS) Lstat(name string) (fs.FileInfo, error) {
	return os.Lstat(filepath.Join(l.root, filepath.FromSlash(name)))
}

func printStartupMessage(cfg config.Config, listeners []net.Listener, localAddr, scheme string) {
	fmt.Println("   ┌─────────────────────────────────────────────┐")
	fmt.Println("   │                                             │")
	fmt.Println("   │   Serving!                                  │")
	fmt.Println("   │                                             │")
	if localAddr != "" {
		fmt.Printf("   │   - Local:    %-30s│\n", localAddr)
	}
	for _, l := range listeners {
		addr := l.Addr().String()
		if strings.HasPrefix(addr, "0.0.0.0:") || strings.HasPrefix(addr, "[::]:") {
			if ip := outboundIP(); ip != "" {
				_, port, _ := net.SplitHostPort(addr)
				fmt.Printf("   │   - Network:  %-30s│\n", scheme+"://"+ip+":"+port)
			}
		}
	}
	fmt.Println("   │                                             │")
	if !cfg.NoClipboard && localAddr != "" {
		fmt.Println("   │   Copied local address to clipboard!        │")
		fmt.Println("   │                                             │")
	}
	fmt.Println("   └─────────────────────────────────────────────┘")
}

func outboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

func printHelp() {
	fmt.Println(`  serve - Static file serving and directory listing

  USAGE
    $ serve --help
    $ serve --version
    $ serve folder_name
    $ serve [-l listen_uri [-l ...]] [directory]

    By default, serve will listen on 0.0.0.0:3000 and serve the
    current working directory on that address.

  OPTIONS
    --help                       Shows this help message
    -v, --version                Displays the current version of serve
    -l, --listen listen_uri      Specify a URI endpoint on which to listen
    -p                           Specify custom port
    -s, --single                 Rewrite all not-found requests to index.html
    -d, --debug                  Show debugging information
    -c, --config                 Specify custom path to serve.json
    -L, --no-request-logging     Do not log any request information
    -C, --cors                   Enable CORS, sets Access-Control-Allow-Origin to *
    -n, --no-clipboard           Do not copy the local address to the clipboard
    -u, --no-compression         Do not compress files
    --no-etag                    Send Last-Modified header instead of ETag
    -S, --symlinks               Resolve symlinks instead of showing 404 errors
    --no-port-switching          Do not open a port other than the one specified when it's taken
    --ssl-cert <path>            PEM cert file for HTTPS (requires --ssl-key)
    --ssl-key <path>             PEM key file for HTTPS (requires --ssl-cert)
    --ssl-pass <path>            File containing passphrase for encrypted PKCS#1 key`)
}
