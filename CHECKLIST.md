# lm-get Feature Checklist

## CLI Core

- [x] Binary builds to single executable
- [x] `--help` / `-h` flag
- [x] `--version` / `-v` flag
- [x] `search <query>` command
- [x] `info <user/repo>` command
- [x] `download <user/repo> <file>` command
- [x] `--dry-run` flag on download
- [x] `-o <dir>` override download directory
- [x] `-n <int>` limit search results
- [x] Default: TUI mode when no arguments

## Search

- [x] Query Hugging Face API
- [x] GGUF-only filtering
- [x] Display model ID, downloads, likes, GGUF count, relative time
- [x] Sort by downloads (default)
- [x] Sort by likes (`--sort likes`)
- [x] Sort by last modified (`--sort lastModified`)
- [x] Sort direction (`--direction`)
- [x] Configurable default sort in config.json
- [x] Configurable results per page
- [x] Pagination (`--page` / `-p` flag)
- [x] Pagination hint ("Use --page N for next page")

## Info / File Listing

- [x] List GGUF files in a repo
- [x] Display file name and size
- [x] RAM color indicator (green = fits, red = exceeds)
- [x] System RAM detection via /proc/meminfo
- [x] mmproj file detection (tagged `[mmproj]`)
- [x] Shard file detection (grouped into single entry)
- [x] Subdirectory scanning for sharded models
- [x] Already-downloaded indicator (tagged `[✓ downloaded]`)
- [x] Truncate long names, scroll when focused (TUI)
- [x] Truncate long names in CLI mode
- [x] Shard groups collapsed with combined size
- [x] Disk space indicator (✗disk if not enough room)

## Download

- [x] Download GGUF files from Hugging Face
- [x] Progress bar (CLI)
- [x] Download resume via Range header
- [x] File saved to `<download_dir>/<user>/<repo>/<file>`
- [x] `--dry-run` simulation
- [x] Disk space check before download
- [x] Download all shards together (shard groups shown as single entry)
- [x] mmproj optional prompt (TUI: after download, asks y/n; CLI: hints about mmproj)
- [x] Progress bar in TUI (real-time updates via channel)
- [x] Integrity check (file size vs expected)
- [x] Retry on network failure (3 attempts with backoff)

## Caching

- [x] API response caching to ~/.cache/lm-get/
- [x] Configurable cache TTL in config.json
- [x] Cache hit avoids network call
- [x] Cache clear command (`lm-get cache clear`)
- [x] Cache info command (`lm-get cache info` — shows size + count)

## Config

- [x] Config file at ~/.config/lm-get/config.json
- [x] downloads_dir setting
- [x] results_per_page setting
- [x] default_sort setting
- [x] cache_ttl_seconds setting
- [x] Config validation on load
- [x] Config init command (`lm-get config init`)

## TUI (Interactive Mode)

- [x] Text input for search query
- [x] Search results list with up/down navigation
- [x] Model detail view (file list)
- [x] Download progress view with real-time updates
- [x] Back navigation (Esc key)
- [x] Quit (q / Ctrl+C)
- [x] Sort toggle (s key) in search view
- [x] Scroll long names when focused
- [x] RAM indicators in detail view
- [x] mmproj tags in detail view
- [x] Shard tags in detail view
- [x] Already-downloaded indicator in detail view
- [x] Split-pane detail view (README left, files right)
- [x] README rendering in TUI (word-wrapped, scrollable)
- [x] Tab to switch focus between README and files
- [x] `?` help overlay
- [x] Pagination (n/p keys in search view)
- [x] mmproj download prompt after model download
- [x] Friendly message when no results found

## Error Handling

- [x] Red `[ERROR]` messages in CLI
- [x] Error display in TUI input view
- [x] HTTP error handling
- [x] JSON parse error handling
- [x] Network retry (3 attempts with exponential backoff)
- [x] Integrity check (file size comparison after download)

## Model Management

- [x] List downloaded models (`lm-get list`)
- [x] Remove/uninstall downloaded model (`lm-get remove`)
- [x] Show disk usage of downloaded models
- [x] Show total disk usage across all models
- [x] Confirmation prompt before remove

## Platform

- [x] Linux only (uses /proc/meminfo, syscall.Statfs)
- [x] No runtime dependencies (static binary)
- [x] Zero external deps beyond bubbletea/lipgloss for TUI

## Build & Distribution

- [x] Go module with go.mod
- [x] Single `go build` produces binary
- [x] Makefile (build, install, uninstall, clean, strip)
- [ ] Shell completion (bash/zsh)
