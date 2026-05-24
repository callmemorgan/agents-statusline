# claude-statusline

A fast statusline renderer for [Claude Code](https://claude.ai/code) and [Antigravity CLI](https://antigravity.dev) (agy).

Both tools pipe a JSON payload to this binary on every turn. It renders a colored, multi-line summary in your terminal. The number of lines is fully configurable — segments are assigned to lines 1–9 and empty lines are collapsed automatically.

The core renderer has no dependencies. The interactive `--configure` TUI uses [tview](https://github.com/rivo/tview).

## Install

```bash
go install github.com/callmemorgan/claude-statusline@latest
```

Or clone and build:

```bash
git clone https://github.com/callmemorgan/claude-statusline.git
cd claude-statusline
go build -o claude-statusline .
```

## Wiring it up

### Claude Code

Set the statusline command in your Claude Code settings:

```bash
claude config set statusLine '{"type":"command","command":"/path/to/claude-statusline"}'
```

### Antigravity CLI (agy)

Add to your agy config:

```json
{
  "statusline": "/path/to/claude-statusline"
}
```

The binary auto-detects which tool is calling it via the `product` field in the payload and hides segments that aren't applicable (e.g. rate limits are hidden under agy, plan tier is hidden under Claude Code).

## Segments

| Segment | Default line | Source |
|---------|-------------|--------|
| `vim-mode` | 1 | Claude Code |
| `sandbox` | 1 | agy |
| `session-name` | 1 | both (UUIDs from agy are trimmed to 8 chars) |
| `agent-state` | 1 | agy |
| `agent-name` | 1 | Claude Code |
| `directory` | 1 | both |
| `git-branch` | 1 | both |
| `artifact-count` | 1 | agy |
| `lines-changed` | 1 | Claude Code |
| `cache-percent` | 1 | Claude Code |
| `plan-tier` | 1 | agy |
| `cost` | 1 | Claude Code |
| `model` | 2 | both |
| `version` | 2 | both |
| `duration` | 2 | Claude Code |
| `api-efficiency` | 2 | Claude Code |
| `tokens` | 2 | both |
| `context-window` | 3 | both |
| `rate-limit-5h` | 3 | Claude Code |
| `rate-limit-7d` | 3 | Claude Code |

Segments that receive no data from the active tool hide themselves automatically — no configuration needed.

## Configuration

```bash
claude-statusline --configure
```

Opens an interactive TUI: a scrollable segment list on the left, a live description panel on the right, and a statusline preview below.

| Key | Action |
|-----|--------|
| `↑` / `↓` | Navigate segments |
| `Space` | Toggle segment on/off |
| `1`–`9` | Move segment to that line (enables it if disabled) |
| `←` / `→` | Reorder segment within its current line |
| `r` | Reset to defaults |
| `s` | Save and exit |
| `q` | Quit without saving |

### Manual config

Config lives at `~/.config/claude-statusline/config.json`:

```json
{
  "segments": [
    "session-name",
    "directory",
    "git-branch",
    "cost",
    "model",
    "version",
    "context-window",
    "rate-limit-5h",
    "rate-limit-7d"
  ],
  "lines": {
    "cost": 2
  }
}
```

- `segments` — which segments to show and in what order. Omit to use defaults.
- `lines` — override which line a segment renders on (1–9). Omit a segment to use its natural line.
- Empty array `[]` — hides the statusline entirely.
- Blank lines (no active segments) are collapsed automatically.

## Plugins

Add your own segments with any executable — a shell script, Python script, or binary. Each plugin runs on every turn, and its stdout becomes the segment content. Empty output hides the segment automatically.

### Single-field plugin

One segment, whole stdout is the value:

```json
{
  "plugins": [
    {
      "id": "memory",
      "command": "~/.config/claude-statusline/plugins/memory.sh",
      "line": 1,
      "desc": "RAM usage",
      "timeout_ms": 200
    }
  ]
}
```

### Multi-field plugin

One command, multiple independent segments. The command runs **once** per turn; each field reads its value from a `key:value` line in stdout:

```json
{
  "plugins": [
    {
      "command": "~/.config/claude-statusline/plugins/memory.sh",
      "timeout_ms": 200,
      "fields": [
        {"id": "mem-used", "line": 1, "desc": "RAM used"},
        {"id": "mem-swap", "line": 1, "desc": "Swap used"},
        {"id": "mem-free", "line": 3, "desc": "Free RAM"}
      ]
    }
  ]
}
```

Each field ID is an independent segment — independently togglable, positionable, and reorderable in the TUI.

- `id` — segment identifier (used in `segments` list and TUI)
- `command` — path to the executable; `~` is expanded
- `line` — default line (1–9); overridable via TUI or `lines` config
- `desc` — shown in the TUI description panel
- `timeout_ms` — kill the process after this many ms (default: 200); hidden if it times out or exits non-zero
- `fields` — multi-field mode; output must use `key:value` lines; mutually exclusive with top-level `id`

Plugin IDs are **auto-appended** to `segments` if not already present, so they appear immediately without editing the list manually.

### Environment variables

The binary exposes these to every plugin:

| Variable | Value |
|----------|-------|
| `STATUSLINE_MODEL` | Model display name |
| `STATUSLINE_DIR` | Current working directory |
| `STATUSLINE_BRANCH` | Git branch |
| `STATUSLINE_SESSION` | Session name or conversation ID |
| `STATUSLINE_PRODUCT` | `antigravity` or empty for Claude Code |
| `STATUSLINE_PAYLOAD` | Full JSON payload (for advanced use) |

### Example: memory + swap (cross-platform, multi-field)

A full working example lives at [`examples/plugins/memory.sh`](examples/plugins/memory.sh). It reports `mem-used`, `swap-used`, and `%-mem-used`, and works on both macOS (`vm_stat`/`sysctl`) and Linux (`/proc/meminfo`).

```sh
cp examples/plugins/memory.sh ~/.config/claude-statusline/plugins/memory.sh
chmod +x ~/.config/claude-statusline/plugins/memory.sh
```

Add to your config:

```json
{
  "plugins": [
    {
      "command": "~/.config/claude-statusline/plugins/memory.sh",
      "timeout_ms": 200,
      "fields": [
        {"id": "mem-used",   "line": 1, "desc": "RAM used"},
        {"id": "swap-used",  "line": 1, "desc": "Swap used"},
        {"id": "%-mem-used", "line": 1, "desc": "RAM % used"}
      ]
    }
  ]
}
```

Plugin segments appear in `--configure` with a `[plugin]` label alongside built-in segments — same toggle, line assignment, and reorder controls.

## Debug

```bash
echo '{"product":"antigravity",...}' | claude-statusline --debug
```

Prints a field presence table comparing the received payload against the Claude Code and agy schemas, plus all parsed values. Useful for diagnosing missing segments or unexpected payload shapes.

## Why Go?

- **Fast** — renders in under 1ms
- **Portable** — single static binary, no runtime
- **Zero core dependencies** — the renderer uses only the standard library; `tview` is only pulled in for `--configure`

## License

MIT
