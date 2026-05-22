# Testing claude-statusline

There are no automated tests in this repo. Because the tool is a pure function from stdin JSON → stdout text, validation is manual.

## Quick sanity check

```bash
go build -o claude-statusline main.go

# Minimal payload — should render 3 lines with defaults
echo '{"model":{"display_name":"Claude"},"workspace":{"current_dir":"~"}}' | ./claude-statusline

# Full payload — should exercise every segment
cat <<'JSON' | ./claude-statusline
{
  "session_name": "test-session",
  "cwd": "/Users/me/code/project",
  "version": "0.42.0",
  "exceeds_200k_tokens": true,
  "model": {"display_name": "Claude 3.7 Sonnet"},
  "workspace": {"current_dir": "/Users/me/code/project", "project_dir": "/Users/me/code/project", "git_worktree": "project"},
  "cost": {"total_cost_usd": 1.23, "total_lines_added": 100, "total_lines_removed": 50, "total_duration_ms": 3661000, "total_api_duration_ms": 2400000},
  "context_window": {"total_input_tokens": 1234567, "total_output_tokens": 89012, "context_window_size": 200000, "used_percentage": 72.5, "current_usage": {"input_tokens": 1200000, "output_tokens": 89012, "cache_creation_input_tokens": 10000, "cache_read_input_tokens": 50000}},
  "rate_limits": {"five_hour": {"used_percentage": 45, "resets_at": 9999999999}, "seven_day": {"used_percentage": 12, "resets_at": 9999999999}},
  "agent": {"name": "ReviewBot"},
  "worktree": {"name": "project", "branch": "feature/test"},
  "vim": {"mode": "normal"},
  "effort": {"level": "high"}
}
JSON
```

## Config behavior

### Default order (no config file)

```bash
rm -f ~/.config/claude-statusline/config.json
echo '{"model":{"display_name":"Claude"},"workspace":{"current_dir":"~"}}' | ./claude-statusline
```

Expect: all 15 segments in default order.

### Custom order

```bash
mkdir -p ~/.config/claude-statusline
cat > ~/.config/claude-statusline/config.json <<'EOF'
{"segments":["model","directory","cost","context-window"]}
EOF
echo '{"model":{"display_name":"Claude"},"workspace":{"current_dir":"~"}}' | ./claude-statusline
```

Expect: only those 4 segments, in that order.

### Hide everything

```bash
echo '{"segments":[]}' > ~/.config/claude-statusline/config.json
echo '{}' | ./claude-statusline
```

Expect: only the timing suffix on line 1, two blank lines below.

### Old boolean config gracefully falls back

```bash
echo '{"show":{"directory":true}}' > ~/.config/claude-statusline/config.json
echo '{}' | ./claude-statusline
```

Expect: defaults (all segments) because the old format is ignored.

### Line overrides

```bash
cat > ~/.config/claude-statusline/config.json <<'EOF'
{
  "segments": ["model", "directory", "cost", "context-window"],
  "lines": {
    "model": 1,
    "cost": 2,
    "context-window": 2
  }
}
EOF
echo '{"model":{"display_name":"Claude"},"workspace":{"current_dir":"~"},"cost":{"total_cost_usd":0.42}}' | ./claude-statusline
```

Expect: `model` on line 1, `directory` on line 1 (natural), `cost` on line 2 (override), `context-window` on line 2 (override).

### Empty lines object = natural lines

```bash
cat > ~/.config/claude-statusline/config.json <<'EOF'
{"segments":["model","cost"],"lines":{}}
EOF
echo '{"model":{"display_name":"Claude"},"cost":{"total_cost_usd":0.42}}' | ./claude-statusline
```

Expect: `model` on line 2 (natural), `cost` on line 1 (natural).

### Invalid line numbers ignored

```bash
cat > ~/.config/claude-statusline/config.json <<'EOF'
{"segments":["model","cost"],"lines":{"model":0,"cost":99}}
EOF
echo '{"model":{"display_name":"Claude"},"cost":{"total_cost_usd":0.42}}' | ./claude-statusline
```

Expect: both segments fall back to their natural lines.

## --configure interactive mode

```bash
# Run configure and type: 5,4,8,9,14  then  done
./claude-statusline --configure

# Verify the saved config
cat ~/.config/claude-statusline/config.json

# Verify the normal pipe respects the new order
echo '{"model":{"display_name":"Claude"},"workspace":{"current_dir":"~"}}' | ./claude-statusline

# Run configure again and type: reset  then  done
./claude-statusline --configure
```

Expect: after `reset`, config is restored to all 15 defaults.

### Line assignment in --configure

```bash
# Start with a minimal config
cat > ~/.config/claude-statusline/config.json <<'EOF'
{"segments":["model","directory","cost"]}
EOF

# Run configure and type:
#   line model 1
#   line cost 2
#   done
./claude-statusline --configure

# Verify the saved config includes lines
cat ~/.config/claude-statusline/config.json
```

Expect: config contains `"lines": {"model": 1, "cost": 2}`. Preview shows `model` on line 1 and `cost` on line 2.

## Edge cases

### Malformed / empty input

```bash
# Empty stdin
echo -n '' | ./claude-statusline

# Not JSON
echo 'not json' | ./claude-statusline

# Missing closing brace
echo '{"model":{' | ./claude-statusline
```

Expect: all cases fall back to minimal output (model="Claude", dir="~").

### Color disable

```bash
# NO_COLOR
NO_COLOR=1 echo '{"model":{"display_name":"Claude"}}' | ./claude-statusline

# TERM=dumb
TERM=dumb echo '{"model":{"display_name":"Claude"}}' | ./claude-statusline
```

Expect: plain text, no ANSI escape codes.

### Outside a git repo

```bash
cd /tmp
echo '{"model":{"display_name":"Claude"},"workspace":{"current_dir":"/tmp"}}' | /path/to/claude-statusline
```

Expect: no git branch segment (even if `git-branch` is in config).

### Zero values

```bash
# No lines changed, no cache, zero cost, zero duration
cat <<'JSON' | ./claude-statusline
{"model":{"display_name":"Claude"},"workspace":{"current_dir":"~"},"cost":{"total_cost_usd":0,"total_lines_added":0,"total_lines_removed":0,"total_duration_ms":0,"total_api_duration_ms":0},"context_window":{"total_input_tokens":0,"total_output_tokens":0}}
JSON
```

Expect: `lines-changed` hidden, `cache-percent` hidden, `api-efficiency` hidden. Cost shows `$0.00`, tokens show `↑0 ↓0`.
