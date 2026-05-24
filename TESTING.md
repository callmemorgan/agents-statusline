# Testing claude-statusline

No automated tests. The tool is a pure function from stdin JSON → stdout text, so validation is manual.

## Build

```bash
go build -o claude-statusline .
```

## Smoke tests

### Minimal payload

```bash
echo '{"model":{"display_name":"Claude"},"workspace":{"current_dir":"~"}}' | ./claude-statusline
```

Expect: default segments render across 3 lines. Segments with no data (cost, rate limits, etc.) are hidden.

### Full Claude Code payload

```bash
cat <<'JSON' | ./claude-statusline
{
  "session_name": "my-project",
  "version": "1.5.0",
  "exceeds_200k_tokens": true,
  "model": {"display_name": "Claude Sonnet 4.6", "id": "claude-sonnet-4-6"},
  "workspace": {"current_dir": "/Users/me/code/my-project", "project_dir": "/Users/me/code/my-project", "git_worktree": "my-project"},
  "cost": {"total_cost_usd": 1.23, "total_lines_added": 100, "total_lines_removed": 50, "total_duration_ms": 3661000, "total_api_duration_ms": 2400000},
  "context_window": {"total_input_tokens": 1234567, "total_output_tokens": 89012, "context_window_size": 200000, "used_percentage": 72.5, "current_usage": {"input_tokens": 1200000, "output_tokens": 89012, "cache_creation_input_tokens": 10000, "cache_read_input_tokens": 50000}},
  "rate_limits": {"five_hour": {"used_percentage": 45, "resets_at": 9999999999}, "seven_day": {"used_percentage": 12, "resets_at": 9999999999}},
  "agent": {"name": "ReviewBot"},
  "worktree": {"name": "my-project", "branch": "feature/test"},
  "vim": {"mode": "normal"},
  "effort": {"level": "high"}
}
JSON
```

Expect: all Claude Code segments visible — vim mode, session name, directory, branch, lines changed, cache %, cost, model with ⬆ badge, version, duration, API efficiency, tokens, context bar with >200k warning, 5h and 7d rate limit bars.

### Full Antigravity (agy) payload

```bash
cat <<'JSON' | ./claude-statusline
{
  "conversation_id": "fbce29fe-0688-4fba-8cc1-0b769834c6d7",
  "product": "antigravity",
  "version": "1.0.2",
  "model": {"display_name": "Gemini 3.5 Flash (High)"},
  "workspace": {"current_dir": "/Users/me/code/my-project", "project_dir": "file:///Users/me/code/my-project"},
  "context_window": {"total_input_tokens": 116778, "total_output_tokens": 35463, "context_window_size": 1048576, "used_percentage": 11.1},
  "agent_state": "tool_use",
  "sandbox": {"enabled": false},
  "artifact_count": 2,
  "plan_tier": "Google AI Pro"
}
JSON
```

Expect: UUID trimmed to `fbce29fe`, `file://` stripped from project path, plan tier visible, agent state visible. No cost, duration, rate limits (not in payload — hidden automatically).

### Debug schema

```bash
echo '{"product":"antigravity","model":{"display_name":"Gemini 3.5 Flash (High)"}}' | ./claude-statusline --debug
```

Expect: field presence table with `✓`/`✗` per field, parsed values printed below. No statusline output.

## Config behavior

### Default (no config file)

```bash
rm -f ~/.config/claude-statusline/config.json
echo '{"model":{"display_name":"Claude"},"workspace":{"current_dir":"~"}}' | ./claude-statusline
```

Expect: default 20 segments in default order.

### Custom segments and order

```bash
echo '{"segments":["model","directory","cost","context-window"]}' > ~/.config/claude-statusline/config.json
echo '{"model":{"display_name":"Claude"},"workspace":{"current_dir":"~"},"cost":{"total_cost_usd":0.42}}' | ./claude-statusline
```

Expect: only those 4 segments in that order.

### Hide everything

```bash
echo '{"segments":[]}' > ~/.config/claude-statusline/config.json
echo '{}' | ./claude-statusline
```

Expect: only the timing suffix, no segment output.

### Line overrides

```bash
cat > ~/.config/claude-statusline/config.json <<'EOF'
{
  "segments": ["model", "directory", "cost", "context-window"],
  "lines": {"cost": 1, "context-window": 2}
}
EOF
echo '{"model":{"display_name":"Claude"},"workspace":{"current_dir":"~"},"cost":{"total_cost_usd":0.42}}' | ./claude-statusline
```

Expect: `cost` on line 1, `context-window` on line 2.

### Blank lines collapse

```bash
cat > ~/.config/claude-statusline/config.json <<'EOF'
{"segments":["model","context-window"],"lines":{"model":1,"context-window":5}}
EOF
echo '{"model":{"display_name":"Claude"},"context_window":{"used_percentage":50,"context_window_size":200000}}' | ./claude-statusline
```

Expect: 2 lines output (lines 2, 3, 4 are empty and collapsed).

## Edge cases

### Malformed / empty input

```bash
echo -n '' | ./claude-statusline
echo 'not json' | ./claude-statusline
echo '{"model":{' | ./claude-statusline
```

Expect: all fall back to minimal output (model="Claude", dir="~").

### Color disable

```bash
NO_COLOR=1 echo '{"model":{"display_name":"Claude"}}' | ./claude-statusline
TERM=dumb echo '{"model":{"display_name":"Claude"}}' | ./claude-statusline
```

Expect: plain text, no ANSI escape codes.

### Outside a git repo

```bash
cd /tmp
echo '{"model":{"display_name":"Claude"},"workspace":{"current_dir":"/tmp"}}' | /path/to/claude-statusline
```

Expect: `git-branch` hidden even if it's in the config.

### Zero values hidden

```bash
echo '{"model":{"display_name":"Claude"},"workspace":{"current_dir":"~"},"cost":{"total_cost_usd":0,"total_duration_ms":0}}' | ./claude-statusline
```

Expect: `cost`, `duration`, `api-efficiency` hidden (zero values suppress those segments).

### agy file:// URI stripping

```bash
echo '{"model":{"display_name":"Gemini"},"workspace":{"current_dir":"/Users/me/code","project_dir":"file:///Users/me/code"}}' | ./claude-statusline
```

Expect: directory renders as `code`, not `file:///Users/me/code`.

### agy UUID session name truncation

```bash
echo '{"conversation_id":"fbce29fe-0688-4fba-8cc1-0b769834c6d7","model":{"display_name":"Gemini"}}' | ./claude-statusline
```

Expect: session name shows `fbce29fe`, not the full UUID.

### Real project name preserved

```bash
echo '{"session_name":"skyslope-convoy","model":{"display_name":"Claude"}}' | ./claude-statusline
```

Expect: session name shows `skyslope-convoy` in full (not truncated — it's not a UUID).

## --configure TUI

```bash
./claude-statusline --configure
```

### Basic toggle

1. Navigate with `↑`/`↓`.
2. Press `Space` to toggle a segment off.
3. Press `s` to save. Verify with `cat ~/.config/claude-statusline/config.json`.

### Line assignment

1. Navigate to `cost`. Press `2`. Verify `[L2]` appears in the list.
2. Navigate to `model`. Press `1`. Verify `[L1]` appears.
3. Press `s`. Verify config contains `"lines": {"cost": 2, "model": 1}`.

### Reorder within line

1. Navigate to two segments on line 1.
2. Press `←`/`→` to swap their order.
3. Verify the preview updates immediately.

### Arbitrary line

1. Navigate to any segment. Press `7`.
2. Verify `[L7]` appears. Preview should show it on its own line.
3. Press `s` and re-run the binary to confirm.

### Reset

Press `r` at any time. Verify all toggles and line assignments return to defaults in the preview.
