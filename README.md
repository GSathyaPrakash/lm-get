# lm-get

Search and download GGUF models from Hugging Face, directly from the terminal.

## Features

- **Interactive TUI** — search, browse, inspect, and download models without leaving the terminal
- **CLI mode** — scriptable commands for search, info, download, list, remove
- **Shard-aware** — detects and groups multi-shard files automatically
- **mmproj detection** — identifies vision/mmproj files and prompts for download
- **Download indicators** — shows which files you already have
- **RAM check** — green/red indicators showing if a model fits in your RAM
- **Disk space check** — verifies free space before downloading
- **Resume support** — continues interrupted downloads
- **API caching** — configurable TTL cache for Hugging Face API responses
- **Zero runtime deps** — static Go binary, works after uninstalling Go

## Build

```bash
make build        # build lm-get binary
make strip        # build + strip symbols (smaller binary)
make install      # install to /usr/local/bin
make uninstall    # remove from /usr/local/bin
```

Requires [Go 1.21+](https://go.dev/dl/). Only two dependencies: [bubbletea](https://github.com/charmbracelet/bubbletea) and [lipgloss](https://github.com/charmbracelet/lipgloss).

## Usage

### Interactive (default)

```bash
lm-get
```

Opens the TUI with:
- **Search field** — type a query and press Enter
- **Results list** — navigate with ↑/↓, Enter to select, `s` to cycle sort, `n`/`p` to page
- **Detail view** — split-pane with README (left) and file list (right), Tab to switch focus
- **Downloaded models panel** — on the home screen, Tab to focus, `s` to sort by size/date, `d` to delete
- **Mouse scroll** — scrollwheel works in all panels

### CLI

```bash
lm-get search qwen                    # search for models
lm-get search qwen --sort likes       # sort by likes
lm-get search qwen --page 2           # second page
lm-get info unsloth/Qwen3-5B-GGUF     # show available quantizations
lm-get download <user/repo> <file>    # download a specific file
lm-get list                           # list downloaded models
lm-get remove <user/repo>             # delete a downloaded model
lm-get cache clear                    # clear API cache
lm-get cache info                     # show cache status
lm-get config init                    # create default config
```

## Configuration

Config file: `~/.config/lm-get/config.json`

Create defaults with `lm-get config init`:

```json
{
  "downloads_dir": "~/Models",
  "results_per_page": 20,
  "default_sort": "downloads",
  "cache_ttl_seconds": 300,
  "run_command": "llama-server --model {model} --port 8080"
}
```

| Field | Default | Description |
|-------|---------|-------------|
| `downloads_dir` | `~/Models` | Where downloaded models are stored |
| `results_per_page` | `20` | Search results per page |
| `default_sort` | `downloads` | Sort field: `downloads`, `likes`, `lastModified` |
| `cache_ttl_seconds` | `300` | API cache time-to-live in seconds |
| `run_command` | `llama-server --model {model} --port 8080` | Command to run a model, `{model}` is replaced with file path |

Cache is stored at `~/.cache/lm-get/`.

## TUI Keybindings

| Key | Action |
|-----|--------|
| `↑`/`k`, `↓`/`j` | Navigate |
| `Enter` | Select / Download |
| `Esc` | Go back / Quit |
| `q` | Back / Quit |
| `s` | Cycle sort (search results or downloaded panel) |
| `d` | Delete model (downloaded panel, with confirmation) |
| `r` | Run model with llama-server (downloaded panel) |
| `n`, `p` | Next / Previous page (search results) |
| `Tab` | Switch focus between panels |
| `?` | Toggle help overlay |
| `Ctrl+C` | Force quit |

## License

MIT
