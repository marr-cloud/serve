# serve Go — Release Automation Design

- **Owner:** meitrix8208@gmail.com
- **Date:** 2026-06-02
- **Project root:** `C:\Users\maurr\workspace\go\serve`
- **Predecessors:** Phases 1–3 merged on `main` (last release-relevant commit: `fc49f6f`).

## 1. Goal

Add a GitHub Actions release workflow that produces signed, checksummed binaries on tag push, and document the install path in the README. After this change a single `git tag vX.Y.Z && git push --tags` produces a GitHub Release with 6 binaries + a checksums file + an auto-generated changelog.

## 2. Non-goals

- Homebrew / scoop / nix / apt / aur packaging. (Manual download or `go install` only for now.)
- Signing (cosign / gpg). Plain checksums.txt is sufficient at this stage.
- Container image. CLI tool, not a service.
- Cross-compilation tweaks beyond the matrix below (FreeBSD, OpenBSD, etc.).
- Auto-update mechanism inside the binary.

## 3. Tool choice

GoReleaser v2 (action `goreleaser/goreleaser-action@v6`). Single `.goreleaser.yml` produces cross-platform binaries, archives, `checksums.txt`, and the GitHub Release with a changelog generated from git log. Industry standard for Go CLIs, single dependency, declarative config.

Hand-rolled GitHub Actions matrix was considered and rejected: it would mean ~80 lines of YAML duplicating GoReleaser's defaults (archive naming, checksum generation, release-create step) without any benefit beyond avoiding the dependency.

## 4. Trigger

Two trigger paths in `release.yml`:

- `on: push: tags: ['v*']` — canonical path. `git tag v1.0.0 && git push --tags` → real release.
- `on: workflow_dispatch:` with a boolean `dry-run` input (default `true`) — used to validate the config without polluting tags. When `dry-run=true`, GoReleaser runs with `--snapshot --clean` (no upload, no tag required).

## 5. File layout

```
.goreleaser.yml                       NEW
.github/workflows/release.yml         NEW
.github/workflows/ci.yml              UNCHANGED
go.mod                                MODIFIED  (module serve → github.com/marr-cloud/serve)
internal/config/config.go             MODIFIED  (const Version → var Version)
**/*.go (every import path)           MODIFIED  (serve/... → github.com/marr-cloud/serve/...)
README.md                             MODIFIED  (Install section)
```

**Module rename is part of this change.** The current `module serve` in `go.mod` is fine for local development but blocks `go install github.com/marr-cloud/serve/cmd/serve@latest`. Renaming to `github.com/marr-cloud/serve` requires updating every import line in the project that references `serve/internal/...` or `serve/cmd/serve`. This is mechanical (a single `sed` invocation in CI-safe form) but touches every Go file.

## 6. `.goreleaser.yml` (full)

```yaml
version: 2

project_name: serve

before:
  hooks:
    - go mod tidy

builds:
  - id: serve
    main: ./cmd/serve
    binary: serve
    env:
      - CGO_ENABLED=0
    goos:   [linux, darwin, windows]
    goarch: [amd64, arm64]
    flags:
      - -trimpath
    ldflags:
      - -s -w
      - -X github.com/marr-cloud/serve/internal/config.Version={{.Version}}

archives:
  - id: serve
    formats: [tar.gz]
    format_overrides:
      - goos: windows
        formats: [zip]
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    files: [README.md, LICENSE*]

checksum:
  name_template: "checksums.txt"

changelog:
  sort: asc
  filters:
    exclude: ["^docs:", "^test:", "^chore:", "^ci:", "Merge pull request"]

release:
  draft: false
  prerelease: auto
```

`-trimpath` removes filesystem paths from binary metadata; `-s -w` strips debug symbols and DWARF info (~30% smaller binary). `CGO_ENABLED=0` produces statically linked Go binaries (no glibc dependency on Linux).

Produces 6 binaries (3 OS × 2 arch). Each archive contains the binary, `README.md`, and any `LICENSE*` file in the repo root. `checksums.txt` is SHA-256 of every archive.

## 7. `.github/workflows/release.yml` (full)

```yaml
name: Release

on:
  push:
    tags: ['v*']
  workflow_dispatch:
    inputs:
      dry-run:
        description: "Run goreleaser in snapshot mode (no upload)"
        type: boolean
        default: true

permissions:
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - uses: goreleaser/goreleaser-action@v6
        with:
          distribution: goreleaser
          version: '~> v2'
          args: ${{ github.event_name == 'workflow_dispatch' && inputs.dry-run && 'release --snapshot --clean' || 'release --clean' }}
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

`fetch-depth: 0` is required for GoReleaser's changelog feature (needs full git history to diff against the previous tag). `contents: write` permission is the minimum needed to create the GitHub Release.

## 8. Version injection

Current state: `internal/config/config.go` declares `const Version = "0.1.0"`. GoReleaser injects the tag via `-X serve/internal/config.Version={{.Version}}` ldflag, but `const` symbols cannot be linker-overridden. Change to:

```go
// Version of the serve binary. Overridden at release time via -ldflags.
var Version = "dev"
```

After this change:
- `go install github.com/marr-cloud/serve/cmd/serve@latest` (no release) → `Version == "dev"`.
- GoReleaser binary at tag `v1.0.0` → `Version == "1.0.0"`.
- Local `go build ./cmd/serve` → `Version == "dev"`.

The `--version` flag (already implemented in F1) will show the real value.

## 9. README install section

Replace the current `## Install` block. New content:

```markdown
## Install

**Pre-built binaries** (recommended): download from the [latest release](https://github.com/marr-cloud/serve/releases/latest). Available for Linux, macOS, and Windows on amd64/arm64. Verify with `checksums.txt`.

**From source:**

```bash
go install github.com/marr-cloud/serve/cmd/serve@latest
```

Or clone and build:

```bash
git clone https://github.com/marr-cloud/serve.git
cd serve
go build -o serve ./cmd/serve
```
```

The `go install` path uses the full module URL (matching how Go users actually call it from the wild) rather than the bare `serve/cmd/serve@latest` of earlier docs.

## 10. Done criteria

- [ ] `.goreleaser.yml` + `release.yml` committed and pushed to main.
- [ ] CI workflow (existing) is untouched; release workflow does NOT run on pushes to main, only on tag push or manual dispatch.
- [ ] `Version` is a `var` so `--version` shows the real tag after release.
- [ ] README points at the Releases page.
- [ ] Manual dispatch with `dry-run=true` completes successfully (validates the GoReleaser config without creating a tag) — verified post-merge.
- [ ] (Optional, user-driven) First real release with `git tag v0.1.0 && git push --tags`.

## 11. Anticipated decisions / edge cases

- **Module rename:** committed in Section 5. The ldflag in Section 6 already uses the new path.
- **First-release version:** the user will choose. v0.1.0 matches the current hardcoded value; v1.0.0 signals "Phase 3 complete, parity with npm `serve`". Not blocking — the workflow handles whatever tag is pushed.
- **`prerelease: auto`:** tags like `v1.0.0-rc1` or `v1.0.0-beta` are marked as prereleases (no "latest" alias). Tags like `v1.0.0` are full releases.
- **Windows arm64:** included in the matrix even though it's rare. CGO is disabled so it compiles fine and the binary works on ARM Windows devices.
