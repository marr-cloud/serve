# serve (Go) — Design Spec

**Date:** 2026-06-01
**Status:** Approved for implementation (Phase 1)
**Owner:** meitrix8208@gmail.com

## Goal

Replicar el paquete npm `serve` (https://npmx.dev/package/serve) en Go, conservando la misma interfaz de línea de comandos y comportamiento observable, aprovechando el stack HTTP de Go para obtener mejor rendimiento que la versión Node.

El producto final es un binario CLI (no una librería pública).

## Non-goals

- Publicar una API Go estable / paquete `pkg/` importable.
- Soporte para HTTP/3.
- Hot-reload de `serve.json` sin reiniciar.
- Métricas Prometheus / endpoints de observabilidad.
- Modo proxy / reverse proxy.

## Estado actual

`main.go` monolítico (~600 LOC) con CLI funcional. Implementa la mayoría de flags de npm `serve` (parseo posicional de directorio, `-l`, `-p`, `-s`, `-d`, `-c`, `-L`, `-C`, `-n`, `-u`, `--no-etag`, `-S`, `--no-port-switching`). Soporta ETag básico, gzip, CORS, port switching, clipboard, symlinks, SPA fallback, directory listing minimal. No hay tests. Contiene varios bugs (ver Sección "Bugs y cambios de comportamiento").

## Estrategia: tres fases

Cada fase es un release independiente (`v0.1`, `v0.2`, `v0.3`).

| Fase | Tema | Entregable principal |
|------|------|----------------------|
| F1   | Core CLI endurecido | Refactor a paquetes, tests, bug fixes |
| F2   | `serve.json` | Paridad con `serve-handler` (rewrites, redirects, headers, cleanUrls, etc.) |
| F3   | Listeners y compresión avanzados | Brotli, HTTPS, Unix sockets, Windows named pipes |

Este spec cubre **Fase 1 en detalle**; Fases 2 y 3 quedan esbozadas para que F1 deje los puntos de inserción correctos.

---

## Fase 1 — Diseño detallado

### 1. Estructura de paquetes

```
serve/
├── cmd/serve/
│   └── main.go                  # wiring: parse flags → build server → run. <50 LOC.
├── internal/
│   ├── config/
│   │   ├── config.go            # struct Config + defaults + validación
│   │   ├── flags.go             # parseo de flags y args posicionales
│   │   └── listen.go            # parser de listen URI
│   ├── handler/
│   │   ├── handler.go           # http.Handler raíz; orquesta middlewares
│   │   ├── files.go             # resolución de path, stat, symlinks, SPA fallback
│   │   ├── directory.go         # listado HTML de directorios
│   │   ├── etag.go              # generación y comparación de ETag
│   │   └── headers.go           # CORS, content-type, cache headers
│   ├── compress/
│   │   ├── compress.go          # interfaz Encoder + negociación Accept-Encoding
│   │   └── gzip.go              # implementación gzip
│   ├── listener/
│   │   ├── listener.go          # crea net.Listener desde Config.Listen
│   │   └── portswitch.go        # lógica de port switching
│   ├── mime/
│   │   └── mime.go              # tabla determinista + DetectContentType fallback
│   └── logx/
│       └── logx.go              # logger de requests + debug
├── docs/superpowers/specs/
├── go.mod
├── go.sum
└── README.md
```

**Reglas de los paquetes:**

- `cmd/serve/main.go`: SOLO orquestación. Sin lógica de negocio.
- `internal/config`: parseo y validación. No abre archivos ni red.
- `internal/handler`: implementa `http.Handler`. Recibe un `fs.FS` y un `Config` por constructor (testeable con `testing/fstest.MapFS`).
- `internal/listener`: traduce strings a `net.Listener`. Aislado para que F3 añada TLS sin tocar el resto.
- `internal/compress`: envuelve `http.ResponseWriter`. F3 añade `brotli.go` al lado de `gzip.go`.
- `internal/mime`: tabla determinista (más rápida y predecible que `mime.TypeByExtension`, que depende de archivos del SO).
- `internal/logx`: log estructurado mínimo (suficiente con `log` stdlib en F1).

**Punto de inserción para F2:** un paquete nuevo `internal/rules` se intercalará entre el middleware de CORS y el servido de archivos. El handler raíz llamará `rules.Apply(req)` que puede reescribir URL, devolver redirect, o anotar headers; luego pasa al servido de archivos.

### 2. Flujo de request

```
HTTP request
   │
   ▼
┌─────────────────────────────────────┐
│ logx.Middleware                     │   log método/path/status/duración
├─────────────────────────────────────┤
│ headers.CORSMiddleware              │   si Config.CORS: setea headers, corta OPTIONS
├─────────────────────────────────────┤
│ [F2] rules.Apply                    │   rewrites / redirects / headers (no en F1)
├─────────────────────────────────────┤
│ compress.Negotiate                  │   inspecciona Accept-Encoding, prepara wrapper lazy
├─────────────────────────────────────┤
│ handler.ServeHTTP                   │
│   1. limpia y valida path           │
│   2. stat (con/sin symlinks)        │
│   3. si dir → index.html o listing  │
│   4. si miss → SPA fallback o 404   │
│   5. resuelve ETag                  │
│   6. If-None-Match → 304            │
│   7. set Content-Type               │
│   8. decisión compresión:           │
│        - tipo compresible?          │
│        - hay Range? → no comprimir  │
│        - tamaño >= 1 KiB?           │
│        - cliente acepta?            │
│   9a. compress=NO → http.ServeContent│  maneja Range, If-Modified-Since, If-Match
│   9b. compress=SÍ → gzip directo     │  sin ServeContent; ver fix #2 abajo
└─────────────────────────────────────┘
```

**Decisiones clave:**

- **Compresión + Range son incompatibles.** Si el request trae `Range:`, saltamos compresión.
- **ETag determinista** basado en `modtime` (unix nanos hex) + `size` hex. Sin md5.
- **`http.ServeContent`** es el motor final porque ya implementa Range, If-Modified-Since e If-Match correctamente.
- **Compresión lazy:** el wrapper de gzip se instala solo si el handler decide comprimir, tras chequear tipo y tamaño. Si el archivo es <1 KiB o ya está comprimido (jpg/png/webp/woff2/zip/gz/br), pasa directo.
- **SPA fallback se restringe:** solo cuando método ∈ {GET, HEAD}, `Accept` contiene `text/html`, y el path no tiene extensión de asset conocida. Lista no exhaustiva de extensiones que vetan el fallback: `.js .css .json .map .png .jpg .jpeg .gif .svg .ico .webp .mp4 .mp3 .woff .woff2 .ttf .otf .pdf .zip .wasm .xml .txt`.

### 3. Bugs y cambios de comportamiento

**Bugs reales** (rompen HTTP correcto):

1. **Gzip + Range incompatibles** (`main.go:364-370`): el código actual comprime ignorando `Range`, devolviendo bytes parciales corruptos. Fix: no comprimir si `r.Header.Get("Range") != ""`.
2. **Gzip rompe ETag/Length:** wrapper actual envuelve body pero `http.ServeContent` ya escribió `Content-Length`. Fix: para gzip, no usar `ServeContent`; en su lugar comprimir manualmente y usar `Transfer-Encoding: chunked` (sin Content-Length para archivos grandes); o comprimir en memoria archivos pequeños y setear Content-Length post-compresión.
3. **ETag con ruta md5 solo en debug** (`main.go:511-522`): lógica muerta. Fix: eliminar; ETag siempre `"<modtime-unix-nanos-hex>-<size-hex>"`.
4. **SPA fallback demasiado agresivo** (`main.go:288-293`): devuelve `index.html` para cualquier 404 (incluyendo `/api/foo.json`). Fix: restringir como en el flujo arriba.
5. **Listen URI mal parseado** (`main.go:158-163`): no maneja `tcp://host:port`, IPv6 `[::1]:3000`, ni da mensajes claros de error. Fix: parser dedicado en `internal/config/listen.go` con tests de tabla.
6. **Path traversal no validado explícitamente:** `filepath.Clean` + `filepath.Join` puede salirse del root en algunos casos en Windows. Fix: tras resolver `filePath`, verificar con `filepath.Rel(rootAbs, filePathAbs)` que no empieza por `..`.
7. **`os.Lstat` con symlinks desactivados pero `os.Open` los sigue** (`main.go:280, 327`): inconsistencia. Fix: si `!Symlinks` y modo es symlink, 404 antes de abrir.
8. **Directory listing sin escape HTML** (`main.go:415`): XSS si un archivo tiene `<script>` en el nombre. Fix: `html.EscapeString` en nombres y URLs.
9. **Port switching imprime puerto viejo en startup message** (`main.go:187-190`): `localAddr` se calcula con `srv.Addr` original, no con el listener real. Fix: derivar `localAddr` del `listener.Addr()` real.
10. **Sin shutdown timeout configurable:** 5s hardcoded. Fix: dejar 5s como constante exportable en `internal/listener`.

**Cambios de comportamiento (no bugs, mejoras):**

- Directory listing: escapa HTML, ordena (carpetas primero, luego alfabético case-insensitive), muestra tamaño + modtime, headers `Cache-Control: no-cache` y `X-Content-Type-Options: nosniff`.
- Logger formato fijo: `<method> <path> <status> <bytes> <duration>`.
- `--debug` añade dirección real del listener tras bind + tiempo de resolución de cada request.

**Lo que NO cambia en F1:**
- Set de flags: idéntico al actual.
- Mensaje de startup ("cajita" ASCII) y mensaje del clipboard.
- Comportamiento de CORS, `--no-clipboard`, `--no-port-switching`.

### 4. Estrategia de testing

**4.1. Unitarios** (paquetes puros, sin red ni FS real):

| Paquete | Casos |
|---------|-------|
| `internal/config` | Parseo de flags; listen URI: `3000`, `:3000`, `host:3000`, `tcp://host:3000`, `[::1]:3000`, errores con mensaje |
| `internal/handler/etag.go` | ETag determinista para mismo (modtime, size); cambia si cualquiera cambia |
| `internal/handler/headers.go` | `contentTypeFor(ext)` cubre tabla + fallback `DetectContentType` |
| `internal/compress` | Matriz Accept-Encoding × Content-Type → gzip / identity |
| `internal/mime` | Tabla determinista |

**4.2. Integración** (handler completo con `testing/fstest.MapFS` y `httptest`):

- GET archivo normal → 200, Content-Type correcto, ETag presente.
- GET con `If-None-Match` igual → 304, sin body.
- GET con `Range: bytes=0-99` → 206, 100 bytes, sin gzip aunque cliente acepte.
- GET con `Accept-Encoding: gzip` de HTML → 200 gzip, ETag, sin Range.
- GET con `Accept-Encoding: gzip` de .jpg → 200 identity (blacklist).
- GET de directorio con `index.html` → sirve index.
- GET de directorio sin index → directory listing HTML, escapa nombres con `<`.
- GET 404 + `--single` + `Accept: text/html` → sirve `index.html`.
- GET 404 + `--single` + `Accept: application/json` → 404 normal.
- GET de symlink con `--symlinks=false` → 404.
- GET con path traversal (`../../etc/passwd`) → 404 / 403.
- OPTIONS con CORS activo → 204 con headers.

**4.3. Listener y E2E**:

- `internal/listener`: bind a `:0`, verificar listener válido. Port switching: ocupar puerto, pedir bind, verificar que cambia a +1.
- Un test E2E en `cmd/serve` que arranca el binario apuntando a `tmpdir`, hace 3-4 requests con `net/http.Client`, verifica respuestas, y shutdown limpio. Smoke test del wiring.

**Estructura de tests:**

```
internal/handler/handler_test.go        # tabla con golden cases
internal/handler/files_test.go          # casos de filesystem
internal/config/listen_test.go          # tabla casos válidos/inválidos
internal/compress/compress_test.go      # matriz Accept-Encoding × Content-Type
cmd/serve/serve_e2e_test.go             # smoke test E2E
```

**Cobertura:** `internal/handler` > 80%, resto > 70%. Cada bug arreglado de la Sección 3 debe tener un test que lo blinde.

**No se testea en F1:**
- Clipboard (depende de librería externa).
- Mensaje de startup con caja ASCII (cosmético).
- `getOutboundIP` (depende de red real; el smoke test lo cubre indirectamente).

### 5. Criterios de cierre F1

- Refactor a la estructura de paquetes de la Sección 1, completo.
- Los 10 bug fixes de la Sección 3 aplicados.
- Suite de tests de la Sección 4 pasando.
- README con uso básico, build, e instrucciones de instalación (`go install`, build binario).
- CI mínimo en `.github/workflows/ci.yml`: `go vet`, `go test ./...`, `go build ./...` en Linux + Windows + macOS.
- Binario funcional y testeado en los tres SO.

Desde la perspectiva del usuario: `serve <dir>` se comporta idéntico al actual (mismos flags, mismo output), pero los bugs listados están arreglados y hay tests.

---

## Fase 2 — `serve.json` (esbozo)

Entregables:

- Paquete `internal/rules` con parser de `serve.json` (schema idéntico al npm: `public`, `cleanUrls`, `rewrites`, `redirects`, `headers`, `directoryListing`, `unlisted`, `trailingSlash`, `renderSingle`, `symlinks`).
- Matcher de patrones glob (`!(foo|bar)`, `**`, `*`) compatible con `path-to-regexp`/`micromatch` que usa npm — vía `github.com/gobwas/glob` o implementación propia con tabla de casos.
- Hook entre middleware de CORS y servido de archivos (`[F2] rules.Apply` en el flujo).
- Flag `--config <path>` ya existe; conectarlo al nuevo parser.
- Tests con fixtures de `serve.json` reales tomadas de proyectos open-source.

Criterio de cierre: un `serve.json` arbitrario válido para npm `serve` funciona idéntico con esta versión.

## Fase 3 — Listeners y compresión avanzados (esbozo)

Entregables:

- Brotli en `internal/compress/brotli.go` (`github.com/andybalholm/brotli`).
- HTTPS: flags `--ssl-cert`, `--ssl-key`, `--ssl-pass`. Listener TLS en `internal/listener/tls.go`.
- Unix domain sockets (`unix:/var/run/serve.sock`) — Linux/macOS.
- Windows named pipes (`pipe:\\.\pipe\serve`) — Windows. Si resulta ruidoso, se documenta como no soportado.
- Tests de integración por SO con build tags.

Criterio de cierre: listeners alternativos arrancan y sirven igual que TCP. Brotli se prefiere a gzip si el cliente lo acepta.

---

## Fuera del roadmap (YAGNI)

- HTTP/2 explícito (Go lo da con TLS automáticamente).
- HTTP/3.
- Hot-reload de `serve.json`.
- Métricas Prometheus.
- Modo proxy / reverse proxy.
- Publicación como librería Go importable.

## Decisiones registradas

| # | Decisión | Razón |
|---|----------|-------|
| 1 | Enfoque por fases (F1→F2→F3) | Valor incremental; F1 ya mejora lo actual sin esperar a serve.json |
| 2 | Solo CLI, no librería | Menor compromiso de API; alcance enfocado |
| 3 | Refactor a `internal/<paquete>` en F1 | Cada paquete con un propósito único, testeable, sin comprometer API pública |
| 4 | ETag basado en (modtime, size) sin md5 | Determinista, rápido, suficiente para uso típico de assets estáticos |
| 5 | Tabla MIME determinista en lugar de `mime.TypeByExtension` | No depende de archivos del SO; comportamiento reproducible cross-platform |
| 6 | `http.ServeContent` como motor de servido cuando no hay compresión | Reusa Range/If-Modified-Since del stdlib |
| 7 | Compresión y Range son mutuamente excluyentes | Sin Content-Length válido si comprimes bytes parciales |
| 8 | SPA fallback restringido a GET/HEAD + Accept: text/html + sin extensión de asset | Evita romper requests a APIs y assets |
| 9 | No formal benchmark vs npm `serve` en F1 | Usuario no lo pidió; "rendimiento de Go" llega como consecuencia del stack |
| 10 | Sin git en el repo actual; spec se escribe pero no se commitea | Repo no inicializado |
