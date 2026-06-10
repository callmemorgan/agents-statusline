# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and run

```bash
go build -o claude-statusline .

# Smoke test
echo '{"model":{"display_name":"Claude"},"workspace":{"current_dir":"~"}}' | ./claude-statusline

# Full Claude Code payload (see TESTING.md for full agy payload)
cat TESTING.md  # contains copy-pasteable test payloads for all cases

# Debug schema detection
echo '{"product":"antigravity","model":{"display_name":"Gemini"}}' | ./claude-statusline --debug

# Interactive config TUI
./claude-statusline --configure
```

No automated tests. All validation is manual; see `TESTING.md` for the exhaustive test suite.

## Architecture

Single-file Go project (`main.go`, ~2600 lines). The binary has three modes:

1. **Default** — reads JSON from stdin, prints a colored multi-line statusline to stdout.
2. **`--configure`** — interactive TUI (tview) to toggle/order/assign segments. Saves to `~/.config/claude-statusline/config.json`.
3. **`--debug`** — prints a field-presence table comparing the received payload against Claude Code and agy schemas.

### Data flow

```
stdin JSON → readInput() → parsePayload() → buildStatusline() → stdout
```

`buildStatusline` iterates `cfg.Segments`, calls each segment's `render()` function, groups results by line number (1–9), then applies reflow logic. The `reflow` config setting controls two modes: `"cascade"` (segments spill greedily across line boundaries) and `"group"` (each logical line wraps independently).

### Segment system

Segments are registered in `allSegmentInfos()` as `segmentInfo` structs:

```go
type segmentInfo struct {
    id           string
    line         int      // natural line (1–9)
    desc         string
    primaryColor string   // palette field name for color override
    render       func(p payload, c palette) (string, bool)
}
```

Each `render` function returns `(text, show)`. Return `("", false)` to hide the segment. Segments auto-hide when their source data is missing or zero — never add explicit tool-type checks.

Renderer functions are named `render<SegmentName>` (e.g., `renderCost`, `renderContextWindow`).

### Global state note

`buildCfg` is a package-level variable set at the start of `buildStatusline`. Segment renderers that need per-segment settings (like `context-window` and `rate-limit-*`) read from `buildCfg` via `settingsFor(buildCfg, id)`. This avoids threading `config` through every render signature.

### Plugin system

Plugins are executable commands defined in config. Single-field plugins return their whole stdout as the segment value. Multi-field plugins output `key:value` lines and are cached per command (so a multi-field plugin runs once per turn regardless of how many fields it has).

Plugins receive context via environment variables: `STATUSLINE_MODEL`, `STATUSLINE_DIR`, `STATUSLINE_BRANCH`, `STATUSLINE_SESSION`, `STATUSLINE_PRODUCT`, `STATUSLINE_COLUMNS`, `STATUSLINE_LINES`, `STATUSLINE_PAYLOAD` (full JSON).

## Key conventions

- **Keep the runtime renderer stdlib-only.** External deps (`tview`, `tcell`, `term`) are confined to `--configure` mode.
- **Versioning**: MAJOR.MINOR.REVISION — not strict SemVer. Bump REVISION for bugfixes and features; MINOR for larger milestones.
- **Colors**: Always respect `NO_COLOR` and `TERM=dumb` (palette fields are empty strings when disabled). Use `palette` struct fields, not hardcoded ANSI codes.
- **Section dividers** in `main.go` use the pattern: `// ─── Section Name ───────────────────────────────────────────────────────────`
- **Config path** is hardcoded to `~/.config/claude-statusline/config.json`.
- **`AGENTS.md` is an identical copy of this file.** When editing `CLAUDE.md`, copy it over `AGENTS.md` so they stay in sync.

## Releases

Releases are cut by pushing a `vX.Y.Z` git tag — `.github/workflows/release.yml` runs GoReleaser (`.goreleaser.yaml`) to build darwin/linux/windows binaries and sign them with cosign. There is no version constant in the code; the `version` segment displays the *calling tool's* version from the payload, not this binary's.

## Adding a new built-in segment

1. Write a `renderXxx(p payload, c palette) (string, bool)` function.
2. Add an entry to `allSegmentInfos()` with the segment's natural line (1–9), description, primary color field name, and render function.
3. Add the segment ID to `defaultConfig()` in the appropriate position.
4. If the segment has configurable sub-features (like the progress bars), add entries to `flyoutFeatures` and implement the relevant `settingsFor` / `applyFlyout*` logic following the `context-window` pattern.
5. Update the segment table in `README.md`, and `config.json.example` if the config schema changed. Add a test payload to `TESTING.md` if the segment reads new payload fields.

## Homebrew vs local binary

`/opt/homebrew/bin/claude-statusline` is the Homebrew install. `./claude-statusline` in the repo root is the local build. When testing changes, build locally and use `./claude-statusline` directly or copy over the Homebrew binary.
