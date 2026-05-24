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
