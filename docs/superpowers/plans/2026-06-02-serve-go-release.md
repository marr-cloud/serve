# serve Go — Release Automation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a GoReleaser-driven GitHub Actions release workflow so a `git tag vX.Y.Z && git push --tags` produces a GitHub Release with 6 binaries + checksums + changelog. Also rename the module to enable `go install`.

**Architecture:** Single `.goreleaser.yml` defines build matrix and archives; `release.yml` runs goreleaser on tag push (real release) or manual dispatch (snapshot dry-run). Module path moves from `serve` to `github.com/marr-cloud/serve` so the `go install` URL resolves. Version is linker-injected into `internal/config.Version`.

**Tech Stack:** GoReleaser v2, `goreleaser/goreleaser-action@v6`, GitHub Actions, Go 1.22.

**Spec:** `docs/superpowers/specs/2026-06-02-serve-go-release-design.md`.

**Branch / worktree:** Use `superpowers:using-git-worktrees` to create branch `release-automation` at worktree `.worktrees/release-automation`.

---

## Task 1: Rename module to `github.com/marr-cloud/serve`

This is a single atomic refactor: `go.mod` line + every import statement that references the old path. 12 `.go` files contain the old import.

**Files:**
- Modify: `go.mod` (module line)
- Modify: every `.go` file matching `grep -rl '"serve/' --include="*.go"` (currently 12 files)
- Test: existing test suite (no new tests; verify nothing breaks)

- [ ] **Step 1: Confirm the affected file count before changing anything**

Run: `grep -rl '"serve/' --include="*.go" | wc -l`
Expected: a number > 0. If 0, the rename has already been applied — skip the rest of this task.

- [ ] **Step 2: Rewrite the module line in `go.mod`**

Run: `go mod edit -module github.com/marr-cloud/serve`
Expected: `go.mod`'s first non-comment line now reads `module github.com/marr-cloud/serve`.

- [ ] **Step 3: Rewrite every import line referencing `serve/`**

Pick one of the following, depending on shell:

**Git Bash / Linux / macOS:**
```bash
find . -name '*.go' -not -path './.git/*' -not -path './.worktrees/*' \
  -exec sed -i 's|"serve/|"github.com/marr-cloud/serve/|g' {} +
```

**PowerShell:**
```powershell
Get-ChildItem -Recurse -Include *.go |
  Where-Object { $_.FullName -notmatch '\\\.git\\|\\\.worktrees\\' } |
  ForEach-Object {
    $c = Get-Content -Raw $_.FullName
    [System.IO.File]::WriteAllText($_.FullName, $c -replace '"serve/', '"github.com/marr-cloud/serve/')
  }
```

Either invocation rewrites every `"serve/internal/..."` and `"serve/cmd/serve"` import to the new path.

- [ ] **Step 4: Run `go mod tidy` to sync the module graph**

Run: `go mod tidy`
Expected: no output; `go.sum` may shift slightly if the rename surfaced any dependency tweaks.

- [ ] **Step 5: Run the full test suite and the build to confirm nothing broke**

Run: `go vet ./... && go test ./... -count=1 && go build ./cmd/serve`
Expected: vet clean, all 8 packages PASS, binary builds.

- [ ] **Step 6: Confirm no stale imports remain**

Run: `grep -rl '"serve/' --include="*.go"`
Expected: empty output (no files match the old prefix anymore).

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "refactor: rename module to github.com/marr-cloud/serve for go install"
```

---

## Task 2: Convert `Version` from `const` to `var` for ldflag injection

The current `const Version = "0.1.0"` cannot be overridden by `-X` ldflag because constants are resolved at compile time. Changing to `var` keeps the same default for `go install` users but lets GoReleaser inject the tag value.

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Read the current declaration**

The file's current top-of-file content begins:
```go
package config

// Version of the serve binary. Updated per release.
const Version = "0.1.0"
```

- [ ] **Step 2: Replace `const Version = "0.1.0"` with `var Version = "dev"`**

In `internal/config/config.go`, change those two lines to:

```go
// Version of the serve binary. Default "dev" for `go install` users;
// release builds override via -ldflags "-X github.com/marr-cloud/serve/internal/config.Version=...".
var Version = "dev"
```

- [ ] **Step 3: Run the build and tests to confirm no caller breaks**

Run: `go build ./cmd/serve && go test ./... -count=1`
Expected: PASS. The `--version` flag now prints `serve version dev` for unreleased builds.

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): Version is now a var so release ldflags can override"
```

---

## Task 3: Add `.goreleaser.yml`

Declarative config for cross-platform binary builds + archives + checksums + changelog.

**Files:**
- Create: `.goreleaser.yml`

- [ ] **Step 1: Write the config**

Create `.goreleaser.yml`:

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

- [ ] **Step 2: Validate the config locally (if `goreleaser` CLI is installed)**

Run: `goreleaser check`
Expected: `config is valid`.

If `goreleaser` is not installed, skip this step — the real validation happens in CI on the dry-run dispatch.

- [ ] **Step 3: Commit**

```bash
git add .goreleaser.yml
git commit -m "build: GoReleaser config (linux/darwin/windows × amd64/arm64)"
```

---

## Task 4: Add the release workflow

GitHub Actions workflow that runs GoReleaser on tag push (real release) or manual dispatch (snapshot dry-run by default).

**Files:**
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: Write the workflow**

Create `.github/workflows/release.yml`:

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

`fetch-depth: 0` is required by GoReleaser to diff against the previous tag for changelog generation. `contents: write` is the minimum permission needed for `goreleaser release` to create the GitHub Release.

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "ci: Release workflow (tag push or manual dry-run dispatch)"
```

---

## Task 5: README install section

Replace the current `## Install` block with one that points at GitHub Releases and uses the real module URL for `go install`.

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Locate the current Install section**

Open `README.md`. Find the block beginning with `## Install` (around line 9) and ending right before `## Usage`.

- [ ] **Step 2: Replace the block**

Replace the entire `## Install` section content with:

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

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: Install section points at Releases and the new module URL"
```

---

## Done criteria

After Task 5, verify in the worktree:

- [ ] `go vet ./...` clean
- [ ] `go test ./... -count=1` green (no new tests; existing suite proves the rename didn't break anything)
- [ ] `go build ./cmd/serve` produces a working binary; `./serve --version` prints `serve version dev`
- [ ] No `.go` file in the repo (excluding `.git/` and `.worktrees/`) contains the string `"serve/internal/` or `"serve/cmd/`
- [ ] `goreleaser check` passes (or skip if not installed locally; the dispatch will validate it)
- [ ] Branch ready to squash-merge into `main` via `superpowers:finishing-a-development-branch`
- [ ] Post-merge: trigger Release workflow via the GitHub UI ("Run workflow" with `dry-run=true`) to validate the GoReleaser config produces all 6 binaries + checksums. After that, the user can `git tag v0.1.0 && git push --tags` for the first real release.
