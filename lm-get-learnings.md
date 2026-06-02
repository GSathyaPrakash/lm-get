# LM Studio CLI Alternative - Learnings & Mistakes

## Goal
Create a **binary** (`lm-get`) that replicates LM Studio's model search and download functionality:
- Search Hugging Face for GGUF models
- Download to `LMSTUDIO_DOWNLOADS/<username>/<repo>/<files>`
- Work without the LM Studio desktop app
- Use `~/.config/lm-get/` directory for user preferences (like other TUI programs)

## New Vision

### Default Behavior: Interactive TUI
- By default, `lm-get` runs in **interactive TUI mode**
- Lightweight, simple TUI framework (ncurses-based or similar)
- `lm-get qwen` shows a list of results with:
  - User/name
  - Download count
  - Likes
  - Relative update time (e.g., "10 mins ago", "5 days ago")
- User navigates with up/down keys and selects a model
- **Intuitive keybindings** — up/down to navigate, enter to select, `q` to quit, `?` for help

### Model Detail View
- **Left half**: README file of the repo, fetched from raw file (like GitHub raw)
- **Right half**: List of all available quantizations
  - Filename displayed
  - **Green** if it fits in system RAM, **red** if it doesn't
  - **Vision symbol** if the model supports multimodal/vision
  - **Shard indicator** if the model has multiple files — download all shards together

### Download Behavior
- Shows a **progress bar** during downloads
- **mmproj is optional** for vision models - prompt user whether to download it
- **mmproj detection** - analyze how LM Studio identifies mmproj files (needs investigation)
- The green/red RAM indicator replaces the need for a default quantization
- User must explicitly choose a model - no silent fallback

### Sorting & Pagination
- **Sort options** (interactive + non-interactive CLI flag): by downloads, likes, or update time
- **Pagination** - browse through results with next/previous

### Error Handling
- Simple: show red `[ERROR]` with a descriptive message

### Additional Notes
- **GGUF-only filter** - only GGUF repos are returned
- **Non-interactive CLI** - every TUI feature has a non-interactive equivalent for development/testing
- **RAM detection** - use any available method to detect system RAM (e.g., `free`, `/proc/meminfo`)
- **No model metadata** - no context length, base model, etc. - keep it simple

### Platform & CLI
- **Linux only**
- **Language** - whatever works best with the chosen TUI framework
- **Dry run** - `--dry-run` flag to simulate downloads without actually downloading
- **Version & help** - `--version` and `--help` flags
- **Dependencies & build/install** - handled during development, not specified here

### Caching & Config
- **API caching** - cache HF API responses to avoid repeated calls
- **`~/.config/lm-get/` directory** - store user preferences (default sort, results per page, etc.)
- **Results per page** - 20 by default

### Download & Storage
- **Download resume** - support resuming interrupted downloads
- **Disk space check** - warn user if disk is full before downloading
- **Uninstall/remove** - ability to remove downloaded models

---

## Mistakes & Lessons Learned

### 1. ❌ `set -euo pipefail` + `[ -z "$query" ] && {` = Silent Exit
**Mistake:** Used `set -e` (exit on error) with `[ -z "$query" ] && { ... }`. When `$query` is not empty, `[ -z "$query" ]` returns exit code 1, and `set -e` kills the script silently.

**Lesson:** With `set -e`, conditional commands like `[ -z "$query" ] && {` will exit if the test fails. Use `if/then/fi` instead:
```bash
# Bad (exits silently):
[ -z "$query" ] && { echo "error"; exit 1; }

# Good:
if [ -z "$query" ]; then
    echo "error"
    exit 1
fi
```

### 2. ❌ Space in URL Parameters
**Mistake:** Built search URL as `search=${query} gguf` - the space in "qwen gguf" broke the URL.

**Lesson:** Always URL-encode query parameters. Replace spaces with `+`:
```bash
search="${query// /+}+gguf"
```

### 3. ❌ Wrong Hugging Face API Response Structure
**Mistake:** Assumed `siblings` was an array of strings (`["file.gguf"]`), but it's an array of objects:
```json
{"siblings": [{"rfilename": "Q4_K_M/model.gguf"}, {"rfilename": "Q8_0/model.gguf"}]}
```

**Lesson:** Always inspect the actual API response before writing filters:
```bash
curl -s "https://huggingface.co/api/models?..." | jq '.[0].siblings'
```

### 4. ❌ `jq` Error: "Cannot index string with string"
**Mistake:** Used `.rfc3744_filename` which doesn't exist. The field is `rfilename`.

**Lesson:** Check the actual field names in the API response. The correct jq filter:
```jq
[.siblings[]? | select(.rfilename | endswith(".gguf")) | .rfilename]
```

### 5. ❌ `jq` Error: "endswith() requires string inputs"
**Mistake:** Tried `endswith(".gguf")` on an array instead of strings.

**Lesson:** Iterate first, then filter:
```jq
# Wrong:
[.siblings | endswith(".gguf")]

# Right:
[.siblings[] | select(.rfilename | endswith(".gguf"))]
```

### 6. ❌ `read` Fails in Non-Interactive Mode
**Mistake:** Script worked for search but crashed at `read -p` because stdin wasn't a terminal.

**Lesson:** For non-interactive testing, add `--auto` or `--select` flags. Or test interactively.

### 10. ❌ Quantization Extraction Logic
**Mistake:** Tried to extract quantization from filename by splitting on `.` and manipulating strings. Didn't account for subdirectory paths.

**Lesson:** Extract quantization from the path prefix, not the filename.

---

## LM Studio Model Structure (Discovered)

```
~/New Volume/Models/          # downloadsFolder from settings.json
├── <username>/
│   └── <repo-name>/
│       ├── model-Q4_K_M.gguf
│       ├── model-Q8_0.gguf
│       └── mmproj-*.gguf     # auto-downloaded if present
```

Key settings from `~/.lmstudio/settings.json`:
```json
{
    "downloadsFolder": "/run/media/loq/New Volume/Models",
    "enableEngineProtocolRuntime": false
}
```

---

## Hugging Face API Endpoints Used

### Search Models
```
GET https://huggingface.co/api/models
    ?full=true
    &search=<query>+gguf
    &sort=downloads
    &direction=-1
    &limit=20
    &type=model
```

### List Repo Files
```
GET https://huggingface.co/api/models/<user>/<repo>/tree/main
```

### Download File
```
GET https://huggingface.co/<user>/<repo>/resolve/main/<path>?download=true
```

---

## Tools Used

| Tool | Purpose |
|------|---------|
| `curl` | HTTP requests to Hugging Face API |
| `jq` | JSON parsing and filtering |
| `bash` | Script logic |
| `grep` | Pattern matching |
| `cut` | Path extraction |
| `sort` | Sorting results |

---

## Architecture

### Config Variables
```bash
DOWNLOADS_DIR="${LMSTUDIO_DOWNLOADS:-<path>}"
HF_TOKEN="${HF_TOKEN:-}"
```

### Functions
| Function | Purpose |
|----------|---------|  
| `info/warn/error` | Color-coded logging helpers |

### Key Design Decisions
- **Idempotent downloads** — skips files that already exist
- **mmproj handling** — optional download for vision models, user is prompted
- **No default model or fallback** — user must explicitly choose
- **RAM-aware display** — green/red indicator tells user what fits
- **TUI-first, CLI-equivalent** — every interactive feature has a non-interactive counterpart

### File Structure Replicated
```
<downloads_dir>/<username>/<repo>/
├── Q4_K_M/model-Q4_K_M-00001-of-00003.gguf
├── Q8_0/model-Q8_0-00001-of-00001.gguf
└── mmproj-bf16.gguf
```

## Key Takeaways

1. **Always inspect API responses** before writing parsers
2. **URL-encode parameters** — spaces break URLs
3. **Be careful with `set -e`** — it silences errors in conditionals
4. **Hugging Face API is well-documented** — use it directly for custom workflows
5. **Test non-interactively** — every TUI feature should work via CLI flags for development
6. **Handle partial downloads** — check file existence before downloading
7. **mmproj is part of the model** — detect and handle it separately
