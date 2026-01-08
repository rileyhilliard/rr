# Phase 4: Distribution & Polish

> **Status:** NOT_STARTED

## Goal

Make `rr` installable and documented. Set up release automation, Homebrew formula, shell completions, and comprehensive documentation. This phase enables users to actually install and use the tool.

## Success Criteria

- [ ] `brew install rileyhilliard/tap/rr` works
- [ ] `go install github.com/rileyhilliard/rr@latest` works
- [ ] Shell completions work in bash/zsh/fish
- [ ] README covers installation and quick start
- [ ] Troubleshooting guide exists
- [ ] JSON Schema published for editor support
- [ ] GitHub releases automated on tag

## Phase Exit Criteria

- [ ] All 7 tasks completed
- [ ] v0.1.0 tag creates GitHub release with binaries
- [ ] `brew install` works from tap
- [ ] Documentation reviewed and accurate

## Context Loading

```bash
# Read before starting:
read README.md
read internal/cli/version.go
read internal/cli/completion.go

# Reference:
read ../../ARCHITECTURE.md               # Lines 1089-1145 for distribution
```

---

## Execution Order

```
┌─────────────────────────────────────────────────────────────────┐
│ Task 1: GoReleaser & GitHub Actions                             │
└─────────────────────────────────────────────────────────────────┘
                              │
          ┌───────────────────┼───────────────────┐
          ▼                   ▼                   ▼
┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐
│ Task 2: Shell   │ │ Task 3: Version │ │ Task 4: Docs    │
│ Completions     │ │ Info & Update   │ │ (README, etc)   │
│ (parallel)      │ │ Check           │ │ (parallel)      │
│                 │ │ (parallel)      │ │                 │
└─────────────────┘ └─────────────────┘ └─────────────────┘
          │                   │                   │
          └───────────────────┼───────────────────┘
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ Task 5: JSON Schema & Install Script                            │
└─────────────────────────────────────────────────────────────────┘
                              │
          ┌───────────────────┴───────────────────┐
          ▼                                       ▼
┌─────────────────────────────┐     ┌─────────────────────────────┐
│ Task 6: Homebrew Tap        │     │ Task 7: Final Testing       │
│ (parallel)                  │     │ & v0.1.0 Release            │
└─────────────────────────────┘     └─────────────────────────────┘
```

---

## Tasks

### Task 1: GoReleaser & GitHub Actions Release

**Context:**
- Create: `.goreleaser.yaml`
- Modify: `.github/workflows/release.yml`

**Steps:**

1. [ ] Create `.goreleaser.yaml`:
   ```yaml
   version: 2
   project_name: rr

   builds:
     - main: ./cmd/rr
       binary: rr
       env:
         - CGO_ENABLED=0
       goos:
         - linux
         - darwin
         - windows
       goarch:
         - amd64
         - arm64
       ldflags:
         - -s -w
         - -X main.version={{.Version}}
         - -X main.commit={{.Commit}}
         - -X main.date={{.Date}}

   archives:
     - format: tar.gz
       name_template: "{{ .ProjectName }}_{{ .Os }}_{{ .Arch }}"
       format_overrides:
         - goos: windows
           format: zip
       files:
         - README.md
         - LICENSE
         - completions/*

   checksum:
     name_template: 'checksums.txt'

   changelog:
     sort: asc
     filters:
       exclude:
         - '^docs:'
         - '^test:'
         - '^chore:'
   ```

2. [ ] Update `.github/workflows/release.yml`:
   ```yaml
   name: Release
   on:
     push:
       tags: ['v*']

   permissions:
     contents: write

   jobs:
     goreleaser:
       runs-on: ubuntu-latest
       steps:
         - uses: actions/checkout@v4
           with:
             fetch-depth: 0
         - uses: actions/setup-go@v5
           with:
             go-version: '1.22'
         - uses: goreleaser/goreleaser-action@v5
           with:
             version: latest
             args: release --clean
           env:
             GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
   ```

3. [ ] Test locally: `goreleaser build --snapshot --clean`
4. [ ] Verify binaries created for all platforms in `dist/`

**Verify:** `ls dist/` shows binaries for darwin/linux/windows, amd64/arm64

---

### Task 2: Shell Completions Package

**Context:**
- Modify: `internal/cli/completion.go`
- Create: `completions/` directory with generated files

**Steps:**

1. [ ] Update `internal/cli/completion.go`:
   - Ensure `completion bash/zsh/fish/powershell` subcommands work
   - Include dynamic completions for tasks and hosts

2. [ ] Create `scripts/generate-completions.sh`:
   ```bash
   #!/bin/bash
   mkdir -p completions
   go build -o rr ./cmd/rr
   ./rr completion bash > completions/rr.bash
   ./rr completion zsh > completions/rr.zsh
   ./rr completion fish > completions/rr.fish
   ./rr completion powershell > completions/rr.ps1
   ```

3. [ ] Add to Makefile: `completions` target

4. [ ] Update GoReleaser to include completions in archives

5. [ ] Document installation in README:
   ```bash
   # Bash
   echo 'eval "$(rr completion bash)"' >> ~/.bashrc

   # Zsh
   echo 'eval "$(rr completion zsh)"' >> ~/.zshrc

   # Fish
   rr completion fish > ~/.config/fish/completions/rr.fish
   ```

**Verify:**
```bash
./rr completion bash > /tmp/rr.bash
source /tmp/rr.bash
rr <TAB>  # Should show commands and tasks
```

---

### Task 3: Version Info & Update Check

**Context:**
- Modify: `cmd/rr/main.go`, `internal/cli/version.go`
- Create: `internal/cli/update.go`

**Steps:**

1. [ ] Update `cmd/rr/main.go`:
   - Define version variables for ldflags injection:
     ```go
     var (
         version = "dev"
         commit  = "none"
         date    = "unknown"
     )
     ```

2. [ ] Update `internal/cli/version.go`:
   - Show full version info:
     ```
     rr v1.0.0
     commit: abc1234
     built: 2025-01-08T12:00:00Z
     go: go1.22.0
     os/arch: darwin/arm64
     ```

3. [ ] Create `internal/cli/update.go`:
   - Check GitHub releases API for newer version
   - Cache check result in `~/.cache/rr/update-check` (1 day TTL)
   - Show notice on `rr version` if outdated:
     ```
     A new version is available: v1.1.0
     Update with: brew upgrade rr
     ```
   - Respect `RR_NO_UPDATE_CHECK=1` env var to disable

4. [ ] Add tests for version parsing and update check

**Verify:**
```bash
./rr version
# Should show version, commit, date, go version, os/arch
```

---

### Task 4: Documentation

**Context:**
- Modify: `README.md`
- Create: `docs/configuration.md`, `docs/troubleshooting.md`
- Reference: `../../ARCHITECTURE.md` for content

**Steps:**

1. [ ] Rewrite `README.md`:
   - Project name and one-line description
   - Demo GIF/screenshot (placeholder: "Coming soon")
   - Installation (Homebrew, go install, binary download)
   - Quick start (init, setup, run)
   - Basic configuration example
   - Command reference (brief, link to full docs)
   - Shell completions setup
   - Link to ARCHITECTURE.md
   - Link to CONTRIBUTING.md

2. [ ] Create `docs/configuration.md`:
   - Full schema reference with all fields
   - Examples for each section
   - Variable expansion reference (`${PROJECT}`, `${USER}`)
   - Default values table
   - Validation rules

3. [ ] Create `docs/troubleshooting.md`:
   - Common errors and solutions
   - SSH connectivity issues
   - rsync not found
   - Lock contention
   - Config validation errors
   - Platform-specific issues
   - `rr doctor` output examples

**Verify:** Review rendered markdown on GitHub

---

### Task 5: JSON Schema & Install Script

**Context:**
- Create: `configs/schema.json`, `scripts/install.sh`

**Steps:**

1. [ ] Create `configs/schema.json`:
   - JSON Schema for `.rr.yaml` validation
   - Descriptions for all fields
   - Enum values for choices (on_fail, color, format)
   - Required fields marked
   - Add `$schema` URL for VS Code integration

2. [ ] Test schema with VS Code YAML extension:
   - Add to `.rr.yaml`: `# yaml-language-server: $schema=./configs/schema.json`
   - Verify completions work

3. [ ] Create `scripts/install.sh`:
   ```bash
   #!/bin/bash
   set -e

   # Detect OS and architecture
   OS=$(uname -s | tr '[:upper:]' '[:lower:]')
   ARCH=$(uname -m)
   case $ARCH in
     x86_64) ARCH="amd64" ;;
     aarch64|arm64) ARCH="arm64" ;;
   esac

   # Get latest version
   VERSION=$(curl -s https://api.github.com/repos/rileyhilliard/rr/releases/latest | grep tag_name | cut -d'"' -f4)

   # Download and install
   URL="https://github.com/rileyhilliard/rr/releases/download/${VERSION}/rr_${OS}_${ARCH}.tar.gz"
   curl -sL "$URL" | tar xz -C /tmp
   sudo mv /tmp/rr /usr/local/bin/rr

   echo "Installed rr ${VERSION}"
   rr --version
   ```

4. [ ] Test on Linux and macOS

**Verify:**
```bash
# Test install script
curl -sSL https://raw.githubusercontent.com/rileyhilliard/rr/main/scripts/install.sh | bash
rr --version
```

---

### Task 6: Homebrew Tap

**Context:**
- Create separate repository: `rileyhilliard/homebrew-tap`
- Update `.goreleaser.yaml` for Homebrew

**Steps:**

1. [ ] Create `rileyhilliard/homebrew-tap` repository on GitHub

2. [ ] Update `.goreleaser.yaml` to add brews section:
   ```yaml
   brews:
     - repository:
         owner: rileyhilliard
         name: homebrew-tap
         token: "{{ .Env.HOMEBREW_TAP_TOKEN }}"
       homepage: https://github.com/rileyhilliard/rr
       description: Remote Runner - sync code and execute commands on remote machines
       license: MIT
       install: |
         bin.install "rr"
       test: |
         system "#{bin}/rr", "--version"
   ```

3. [ ] Add `HOMEBREW_TAP_TOKEN` secret to main repo (PAT with repo access)

4. [ ] Test with release tag

**Verify:**
```bash
brew tap rileyhilliard/tap
brew install rr
rr --version
```

---

### Task 7: Final Testing & v0.1.0 Release

**Context:**
- Full project verification

**Steps:**

1. [ ] Run full test suite:
   ```bash
   make lint
   make test
   make test-integration
   ```

2. [ ] Manual testing checklist:
   - [ ] `rr init` creates valid config
   - [ ] `rr setup <host>` works
   - [ ] `rr sync` syncs files
   - [ ] `rr run "command"` works end-to-end
   - [ ] `rr exec "command"` works
   - [ ] `rr status` shows hosts
   - [ ] `rr doctor` runs checks
   - [ ] Error messages are helpful
   - [ ] Help text is accurate

3. [ ] Review all error messages for clarity

4. [ ] Update CHANGELOG.md with v0.1.0 changes

5. [ ] Create and push v0.1.0 tag:
   ```bash
   git tag -a v0.1.0 -m "Initial release"
   git push origin v0.1.0
   ```

6. [ ] Verify release appears on GitHub with:
   - [ ] Binaries for all platforms
   - [ ] Checksums file
   - [ ] Changelog in release notes

7. [ ] Test installation methods:
   - [ ] `brew install rileyhilliard/tap/rr`
   - [ ] `go install github.com/rileyhilliard/rr@latest`
   - [ ] Install script

**Verify:**
```bash
# Full workflow test after release
brew install rileyhilliard/tap/rr
rr init
rr setup mini
rr run "echo hello"
rr doctor
```

---

## Verification

After all tasks complete:

```bash
# Test all installation methods
brew install rileyhilliard/tap/rr
rr --version

go install github.com/rileyhilliard/rr@latest
rr --version

curl -sSL https://raw.githubusercontent.com/rileyhilliard/rr/main/scripts/install.sh | bash
rr --version

# Test completions
rr completion bash > ~/.rr-completion.bash
echo 'source ~/.rr-completion.bash' >> ~/.bashrc
source ~/.bashrc
rr <TAB>

# Test full workflow
rr init
rr setup mini
rr status
rr doctor
rr sync
rr run "ls -la"
```
