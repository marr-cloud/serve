package config

import (
	"flag"
	"fmt"
	"io"
)

type listenSlice []string

func (l *listenSlice) String() string     { return fmt.Sprintf("%v", []string(*l)) }
func (l *listenSlice) Set(v string) error { *l = append(*l, v); return nil }

// ParseFlags consumes args (including program name at index 0) and returns a
// populated Config. Errors are returned for unrecognized flags.
func ParseFlags(args []string) (Config, error) {
	var cfg Config
	var listen listenSlice

	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	fs.Var(&listen, "l", "Specify a URI endpoint on which to listen")
	fs.Var(&listen, "listen", "Specify a URI endpoint on which to listen")
	fs.IntVar(&cfg.Port, "p", 0, "Specify custom port")
	fs.BoolVar(&cfg.Single, "s", false, "Rewrite all not-found requests to index.html")
	fs.BoolVar(&cfg.Single, "single", false, "Rewrite all not-found requests to index.html")
	fs.BoolVar(&cfg.Debug, "d", false, "Show debugging information")
	fs.BoolVar(&cfg.Debug, "debug", false, "Show debugging information")
	fs.StringVar(&cfg.ConfigFile, "c", "", "Specify custom path to serve.json")
	fs.StringVar(&cfg.ConfigFile, "config", "", "Specify custom path to serve.json")
	fs.BoolVar(&cfg.NoRequestLogging, "L", false, "Do not log any request information")
	fs.BoolVar(&cfg.NoRequestLogging, "no-request-logging", false, "Do not log any request information")
	fs.BoolVar(&cfg.CORS, "C", false, "Enable CORS, sets Access-Control-Allow-Origin to *")
	fs.BoolVar(&cfg.CORS, "cors", false, "Enable CORS, sets Access-Control-Allow-Origin to *")
	fs.BoolVar(&cfg.NoClipboard, "n", false, "Do not copy the local address to the clipboard")
	fs.BoolVar(&cfg.NoClipboard, "no-clipboard", false, "Do not copy the local address to the clipboard")
	fs.BoolVar(&cfg.NoCompression, "u", false, "Do not compress files")
	fs.BoolVar(&cfg.NoCompression, "no-compression", false, "Do not compress files")
	fs.BoolVar(&cfg.NoETag, "no-etag", false, "Send Last-Modified header instead of ETag")
	fs.BoolVar(&cfg.Symlinks, "S", false, "Resolve symlinks instead of showing 404 errors")
	fs.BoolVar(&cfg.Symlinks, "symlinks", false, "Resolve symlinks instead of showing 404 errors")
	fs.BoolVar(&cfg.NoPortSwitching, "no-port-switching", false, "Do not open a port other than the one specified when it's taken")
	fs.BoolVar(&cfg.Help, "help", false, "Shows this help message")
	fs.BoolVar(&cfg.Version, "v", false, "Displays the current version of serve")
	fs.BoolVar(&cfg.Version, "version", false, "Displays the current version of serve")

	if err := fs.Parse(args[1:]); err != nil {
		return Config{}, err
	}
	cfg.Listen = []string(listen)
	if positional := fs.Args(); len(positional) > 0 {
		cfg.Directory = positional[0]
	}
	return cfg, nil
}
