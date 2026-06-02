# serve (Go)

A static file server that aims for behavioral parity with the npm `serve` CLI, implemented in Go for a faster runtime and a single self-contained binary.

## Status

Phase 2 complete: `serve.json` parity (rewrites, redirects, headers, cleanUrls, trailingSlash, directoryListing, unlisted, renderSingle, public, symlinks). Phase 3 (Brotli, HTTPS, Unix sockets) is the next milestone. See `docs/superpowers/specs/` for the full roadmap.

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
| `-v`, `--version` | Print version and exit |
| `--help` | Print help and exit |

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

- `gzip` and `Range` are mutually exclusive: a request with a `Range` header is served identity-encoded even if a `headers` rule sets `Content-Encoding`.
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
