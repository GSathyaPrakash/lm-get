# AGENTS.md

## Project Overview

**lm-get** is a Go CLI tool for searching and downloading GGUF machine learning models from Hugging Face. It has two modes: an interactive TUI (default) and scriptable CLI commands.

- **Language**: Go 1.21+
- **Module**: `github.com/loq/lm-get`
- **External deps**: [bubbletea](https://github.com/charmbracelet/bubbletea) + [lipgloss](https://github.com/charmbracelet/lipgloss) (TUI only)
- **Platform**: Linux only (uses `/proc/meminfo`, `syscall.Statfs`)

## Build & Run

```bash
make build          # builds ./lm-get from ./cmd/lm-get/
go build -o lm-get ./cmd/lm-get/
go vet ./...        # lint
go mod tidy         # clean deps
```

## Project Structure

```
lm-get/
├── cmd/
│   └── lm-get/
│       └── main.go           # Entry point: parses args, dispatches to CLI/TUI
├── internal/
│   ├── models/
│   │   └── models.go         # Shared data types (HFModel, DetailEntry, LocalModel, etc.)
│   ├── config/
│   │   └── config.go         # Config loading, validation, defaults (~/.config/lm-get/config.json)
│   ├── cache/
│   │   └── cache.go          # API response caching (file-based, TTL-aware, ~/.cache/lm-get/)
│   ├── api/
│   │   └── api.go            # Hugging Face API client, file parsing, shard detection
│   ├── download/
│   │   └── download.go       # File downloading with resume, progress, disk-space checks
│   ├── display/
│   │   └── display.go        # Terminal formatting: colors, sizes, progress bars, text utils
│   ├── cli/
│   │   └── cli.go            # CLI command handlers (search, info, download, list, remove, help)
│   ├── server/
│   │   └── state.go          # Persistent server state (PID, log path, alive checks)
│   └── tui/
│       └── tui.go            # Interactive Bubble Tea TUI application
├── go.mod / go.sum
├── Makefile
├── PKGBUILD                   # Arch Linux package
├── README.md
├── CHECKLIST.md               # Feature checklist
└── lm-get-learnings.md        # Design notes and API documentation
```

## Package Dependency Graph

```
cmd/lm-get/main.go
├── internal/api          (Version string)
├── internal/cache        (CmdCache)
├── internal/cli          (CmdSearch, CmdInfo, CmdDownload, CmdList, CmdRemove, PrintHelp, ScanLocalModels, SortLocalModels)
├── internal/config       (CmdConfigInit)
├── internal/display      (Red, Reset)
└── internal/tui          (Run)

internal/tui
├── internal/api
├── internal/cli          (ScanLocalModels, SortLocalModels)
├── internal/config
├── internal/display
├── internal/download
├── internal/models
└── internal/server

internal/cli
├── internal/api
├── internal/config
├── internal/display
├── internal/download
└── internal/models

internal/api
├── internal/cache
├── internal/config
├── internal/display
└── internal/models

internal/download
├── internal/api
├── internal/config
└── internal/display

internal/cache
├── internal/config
└── internal/display
```

No circular dependencies. Packages only depend on packages below them in this list.

## Package Responsibilities

### `internal/models` — Data Types
All shared struct types used across packages. No logic, no imports from other internal packages.
- `HFModel` — Hugging Face model from search API
- `HFFileTree` — File tree entry from repo API
- `ModelFile` — Parsed model file with shard/mmproj metadata
- `DetailEntry` — Grouped display entry (single file, shard group, or mmproj)
- `LocalModel`, `LocalFileEntry` — Downloaded model on disk
- `LocalSort` — Sort enum for local models

### `internal/config` — Configuration
Reads/writes `~/.config/lm-get/config.json`. Validates all fields with sensible defaults.
- `Load()` — Load config (returns defaults if file missing)
- `Save()` — Write config to disk
- `Default()` — Return default config
- `CmdConfigInit()` — CLI handler for `lm-get config init`

Config fields: `downloads_dir`, `results_per_page`, `default_sort`, `cache_ttl_seconds`, `run_command`, `parallel_connections`.

### `internal/cache` — API Response Cache
File-based cache at `~/.cache/lm-get/`. JSON entries with TTL from config.
- `Get(url)` — Return cached data if fresh
- `Set(url, data)` — Store response
- `Clear()`, `Info()` — Cache management
- `CmdCache(args)` — CLI handler for `lm-get cache [clear|info]`

### `internal/api` — Hugging Face API Client
HTTP client with retry (3 attempts, backoff). All HF API interaction.
- `SearchModels(query, limit, sort, direction, page)` — Search GGUF models
- `ListFiles(repo)` — List GGUF files in a repo (includes subdirectories)
- `ParseModelFiles()` — Convert raw file tree to `ModelFile` with shard detection
- `BuildDetailEntries()` — Group files into `DetailEntry` (shard groups, mmproj, singles)
- `FetchReadme(repo)` — Fetch README.md
- `GetDownloadURL(repo, path)` — Build download URL
- `IsFileDownloaded(repo, path)` — Check if file exists locally
- `HasMmprojFiles(repo)` — Check if repo has vision files
- `GetDiskFree(path)` — Check available disk space
- `Version` — Set by main at startup (for User-Agent header)

### `internal/download` — File Download
Downloads files with parallel range requests (default 8 connections, `parallel_connections` in config), crash-safe resume via a `.lmget-state` sidecar tracking completed byte ranges, per-segment retries, progress tracking, disk-space verification.
- `File(url, dest)` — Download with CLI progress bar
- `FileWithRetries(url, dest, attempts)` — File with whole-engine retries
- `MultipleFiles(repo, paths, outputDir)` — Download shard group
- `ToFile(url, dest, progressChan)` — Download with channel-based progress (for TUI)

### `internal/display` — Terminal Utilities
Pure formatting functions. No state. Used by all packages.
- ANSI color constants: `Red`, `Green`, `Bold`, `Reset`, etc.
- `FormatSize()`, `FormatNumber()` — Human-readable sizes/numbers
- `RelativeTime()` — "3 days ago" formatting
- `ProgressBar()` — ASCII progress bar
- `RAMColor()` — Green/red based on RAM fit
- `Truncate()`, `ExpandHome()`, `DirSize()`, `WordWrap()`, `StripMarkdown()`, `StripHTML()`
- `PrintError()` — Formatted error to stderr
- `GetSystemRAM()` — Read system RAM from `/proc/meminfo`

### `internal/cli` — CLI Command Handlers
One function per CLI subcommand. Each parses args, calls api/download, formats output.
- `CmdSearch(args)` — `lm-get search <query> [flags]`
- `CmdInfo(args)` — `lm-get info <user/repo>`
- `CmdDownload(args)` — `lm-get download <user/repo> <file> [flags]`
- `CmdList(args)` — `lm-get list`
- `CmdRemove(args)` — `lm-get remove <user/repo>`
- `PrintHelp()` — Usage text
- `ScanLocalModels()` — Scan download dir for local models
- `SortLocalModels()` — Sort by time or size

### `internal/tui` — Interactive TUI
Full Bubble Tea application. Handles all TUI states, keybindings, rendering.
- `Run()` — Start the TUI (called from main when no args)
- States: Input → Loading → Search → Loading → Detail → Downloading → MmprojPrompt
- Also manages: local models panel, server process launching, log viewing

### `internal/server` — Server State Persistence
Tracks running llama-server processes across sessions via `~/.cache/lm-get/servers.json`.
- `Load()` — Load state, prune dead PIDs
- `Save()` — Write state to disk
- `Set(modelPath, entry)` — Register a running server
- `Remove(modelPath)` — Remove a server entry
- `IsAlive(pid)` — Check if PID is still running (via `kill(pid, 0)`)
- `LogDir()` — Returns `~/.cache/lm-get/logs/`

## Key APIs (Hugging Face)

| Endpoint | Purpose |
|----------|---------|
| `GET /api/models?search=<q>+gguf&sort=downloads&full=true` | Search models |
| `GET /api/models/<repo>/tree/main` | List repo files |
| `GET /<repo>/resolve/main/<path>?download=true` | Download file |
| `GET /<repo>/raw/main/README.md` | Fetch README |

## Key Conventions

- All exported functions are capitalized, all internal helpers are lowercase
- Config is loaded fresh on each call (no global state beyond cache dir)
- Error handling: CLI uses `display.PrintError()`, TUI shows in `m.err`
- Downloads go to `<downloads_dir>/<user>/<repo>/<file>.gguf`
- Cache entries are keyed by URL hash, stored as JSON in `~/.cache/lm-get/`

## Release Process

### Version locations to update
1. `cmd/lm-get/main.go` — `var version = "x.y.z"`
2. `PKGBUILD` — `pkgver=x.y.z`

### Push to GitHub
```bash
git add -A
git commit -m "v0.x.y: <summary>"
git tag v0.x.y
git push origin main --tags
```

### Push to AUR
AUR repo is at `~/git/lm-get-aur/` (cloned from `ssh://aur@aur.archlinux.org/lm-get.git`).
```bash
cd ~/git/lm-get-aur/
# Update PKGBUILD pkgver=x.y.z
# Update .SRCINFO pkgver and source URL to match
git add PKGBUILD .SRCINFO
git commit -m "Update to x.y.z"
git push origin master
```
