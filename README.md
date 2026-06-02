# serve (Go)

A static file server that aims for behavioral parity with the npm `serve` CLI, implemented in Go for a faster runtime and a single self-contained binary.

## Status

Phase 3 complete: Brotli compression, HTTPS (`--ssl-cert`/`--ssl-key`/`--ssl-pass`), Unix domain sockets (Linux/macOS), and Windows named pipes are now supported in addition to the Phase 2 `serve.json` parity. See `docs/superpowers/specs/` for the full roadmap.

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
| `-c`, `--config <path>` | Path to `serve.json` (see [serve.json](#servejson)) |
| `-L`, `--no-request-logging` | Suppress per-request log lines |
| `-C`, `--cors` | Enable CORS (`Access-Control-Allow-Origin: *`) |
| `-n`, `--no-clipboard` | Don't copy local URL to clipboard |
| `-u`, `--no-compression` | Disable gzip compression |
| `--no-etag` | Send `Last-Modified` instead of `ETag` |
| `-S`, `--symlinks` | Resolve symlinks instead of returning 404 |
| `--no-port-switching` | Fail instead of trying successor ports when the requested port is taken |
| `--ssl-cert <path>` | PEM cert file for HTTPS (requires `--ssl-key`) |
| `--ssl-key <path>` | PEM private key file for HTTPS |
| `--ssl-pass <path>` | File containing passphrase for an encrypted PKCS#1 key. PKCS#8-encrypted keys are not supported; convert with `openssl pkcs8 -in key.pem -traditional -out key.pkcs1.pem` |
| `-v`, `--version` | Print version and exit |
| `--help` | Print help and exit |

## HTTPS

```bash
serve --ssl-cert ./cert.pem --ssl-key ./key.pem ./public
```

For an encrypted key, place the passphrase in a file (no trailing newline) and pass `--ssl-pass`. Only PKCS#1 PEM (`BEGIN RSA PRIVATE KEY` with a `Proc-Type` header) is supported. Modern openssl produces PKCS#8 by default; convert with `openssl pkcs8 -in key.pem -traditional -out key.pkcs1.pem`.

TLS minimum is 1.2. There's no support for autocert / Let's Encrypt — front with Caddy or nginx for that.

## Alternative listeners

Beyond TCP, the `-l` flag accepts:

```bash
serve -l unix:/tmp/serve.sock ./public         # Linux/macOS only
serve -l "pipe:\\.\pipe\serve" ./public        # Windows only
```

Unix sockets are created with mode `0660` and removed on shutdown. Named pipes use the SDDL `D:P(A;;GA;;;WD)` (allow `Everyone`), appropriate for a local-only file server. TLS is wrapped around TCP/unix listeners when `--ssl-cert` is set; pipes are local-only and not wrapped.

## serve.json

If a `serve.json` exists in the served directory (or you pass `-c <path>`), `serve` reads it for behavior overrides. The schema mirrors npm `serve-handler`:

```json
{
  "public": "./dist",
  "cleanUrls": true,
  "trailingSlash": false,
  "renderSingle": false,
  "directoryListing": true,
  "unlisted": [".git", "*.bak"],
  "redirects": [
    {"source": "/legacy/:slug", "destination": "/blog/:slug", "type": 301}
  ],
  "rewrites": [
    {"source": "/api/:version/users", "destination": "/data/users-:version.json"}
  ],
  "headers": [
    {"source": "/**.css", "headers": [{"key": "Cache-Control", "value": "public, max-age=31536000"}]}
  ],
  "symlinks": false
}
```

**Pattern syntax** (`path-to-regexp` v6 subset): `:name`, `:name?`, `:name+`, `:name*`, `*`, `**`. Captures are referenced in destinations with `:name` or `$N` (positional).

**Rule order on each request:** redirects → rewrites (single-pass) → cleanUrls → trailingSlash → file lookup → matching `headers` rules applied to the response.

**Precedence:** CLI flags > `serve.json` > defaults. Passing `serve ./other` overrides `"public": "./dist"`; passing `-S` overrides `"symlinks": false`.

**Legacy:** `now.json` (the predecessor of `serve.json`) is recognized when `serve.json` is absent.

## Behavioral notes

- Compression auto-detection: when the client sends `Accept-Encoding: br, gzip`, `serve` emits brotli (preference `br > gzip > identity` on ties; explicit q-values still win). `*` matches any encoding and resolves to brotli.
- `Range` requests are served identity-encoded regardless of `Accept-Encoding` — both gzip and brotli are skipped when a `Range` header is present.
- SPA fallback (`-s`) only applies to GET/HEAD requests with `Accept: text/html` and a URL path without a known asset extension. Requests to `/api/*` or `*.json` return 404 normally.
- ETag is `"<modtime-unix-nanos>-<size>"`, deterministic across processes and machines.
- Directory listings are HTML-escaped (no XSS via filenames).
- A bare `/dir` request 301-redirects to `/dir/`, except when `trailingSlash: false` is in `serve.json`.

## Tests

```bash
go test ./...
```

## License

See LICENSE.
