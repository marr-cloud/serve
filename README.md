# serve (Go)

A static file server that aims for behavioral parity with the npm `serve` CLI, implemented in Go for a faster runtime and a single self-contained binary.

## Status

Phase 1 (core CLI hardened). `serve.json` support is planned for Phase 2; Brotli, HTTPS, and Unix sockets are planned for Phase 3. See `docs/superpowers/specs/2026-06-01-serve-go-design.md` for the full roadmap.

## Install

```bash
go install serve/cmd/serve@latest
```

Or build from source:

```bash
git clone <repo>
cd serve
go build -o serve ./cmd/serve
```

## Usage

```bash
serve                  # serve the current directory on :3000
serve ./public         # serve ./public on :3000
serve -p 8080 ./dist   # custom port
serve -s ./dist        # SPA mode (HTML fallback)
serve -C ./public      # enable CORS
serve -l :4000 -l :4001 ./public  # multiple listen addresses
```

## Flags

| Flag | Description |
|------|-------------|
| `-l`, `--listen <addr>` | URI endpoint to listen on (may repeat). Accepts `port`, `:port`, `host:port`, `tcp://host:port`, `[::1]:port`. |
| `-p <port>` | Custom port (alias for `-l :port`) |
| `-s`, `--single` | Rewrite missing routes to `index.html` (SPA mode) |
| `-d`, `--debug` | Verbose debug output |
| `-c`, `--config <path>` | Path to `serve.json` (planned, Phase 2) |
| `-L`, `--no-request-logging` | Suppress per-request log lines |
| `-C`, `--cors` | Enable CORS (`Access-Control-Allow-Origin: *`) |
| `-n`, `--no-clipboard` | Don't copy local URL to clipboard |
| `-u`, `--no-compression` | Disable gzip compression |
| `--no-etag` | Send `Last-Modified` instead of `ETag` |
| `-S`, `--symlinks` | Resolve symlinks instead of returning 404 |
| `--no-port-switching` | Fail instead of trying successor ports when the requested port is taken |
| `-v`, `--version` | Print version and exit |
| `--help` | Print help and exit |

## Behavioral notes

- `gzip` and `Range` are mutually exclusive: a request with a `Range` header is served identity-encoded.
- SPA fallback (`-s`) only applies to GET/HEAD requests with `Accept: text/html` and a URL path without a known asset extension. Requests to `/api/*` or `*.json` return 404 normally.
- ETag is `"<modtime-unix-nanos>-<size>"`, deterministic across processes and machines.
- Directory listings are HTML-escaped (no XSS via filenames).

## Tests

```bash
go test ./...
```

## License

See LICENSE.
