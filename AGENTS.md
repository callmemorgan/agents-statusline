# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and test

The `Makefile` wraps the common gates; prefer it over raw commands when running the whole suite:

```bash
make build       # go build -o agents-statusline ./cmd/agents-statusline
make check       # full pre-commit gate: lint (Go + JS/TS) + vet + test
make lint        # golangci-lint ./... AND biome check on npm/ + scripts/
make fmt         # gofmt + goimports, and biome format on npm/ + scripts/
make install-tools  # golangci-lint (brew) + biome (npm install)
```

Go is the primary language; the JS/TS (npm shim, `scripts/*.mjs`, Pi extension) is linted/formatted with Biome, not gofmt. Run `make check` before publishing changes.

```bash
go build -o agents-statusline ./cmd/agents-statusline
go test ./...                      # full suite (golden, migration, state, install splicer…)
go test -run Golden -update ./internal/render  # regenerate golden files after intentional render changes
go test -run TestSessionStateRecordSaveLoad ./internal/state  # single test

# Smoke test
echo '{"model":{"display_name":"Claude"},"workspace":{"current_dir":"~"}}' | ./agents-statusline

# Schema/config debugging
./agents-statusline debug < testdata/payloads/agy-full.json

# Interactive config TUI (the one thing tests don't cover — verify manually)
./agents-statusline configure
```

Golden tests render `testdata/payloads/* × configs` with an empty palette (color-free) and a fixed clock (`testNow`); fixtures use `resets_at` values relative to that clock so countdowns are deterministic. TESTING.md keeps copy-pasteable payloads for manual verification, mainly of the TUI.

**Careful when smoke-testing locally:** running `./agents-statusline` with no `config.toml` but an existing `config.json` migrates the real config (renames it to `.bak`). Use an isolated home: `HOME=/tmp/fake-home ./agents-statusline`.

## Architecture

One Go module, `package main` entry point in `cmd/agents-statusline/`, split by concern under `internal/`. The binary's subcommands (`cmd/agents-statusline/cmd.go` dispatch): bare stdin→stdout rendering (how Claude Code invokes it — must never change behavior), `install`/`uninstall`, `configure` (tview TUI), `version`, `debug`, `help`.

### Data flow

```
stdin JSON → readInput() → parsePayload() ┐
config.toml → loadConfigWarn() ───────────┼→ render.Statusline(render.Input) → stdout
state file  → loadState()/Record() ───────┘            └→ st.Save() after printing
```

`render.Statusline` (`internal/render/render.go`) iterates `cfg.Segments`, builds a `segments.RenderCtx` per segment (payload, override-applied palette, resolved settings, optional state, injected clock), groups results by line (1–9), then reflows. Wrapping is **opt-in**: the default (`off`/`""`, via `buildStatuslineNoWrap` = cascade with no column budget) emits each logical line as-is and lets the terminal soft-wrap; `cascade` spills segments across line boundaries; `group` wraps each logical line independently.

### Key subsystems and their files

- **Segments** (`internal/segments`) — `segments.Info` registry in `allSegmentInfos()`; renderers are `func(ctx segments.RenderCtx) (string, bool)`; return `("", false)` to hide. Segments auto-hide when source data is missing/zero — never add tool-type checks.
- **Settings schema** (`schema.go`) — each segment declares `[]settingSpec` (kind bool/int/enum/color, default, bounds). `settingsFor` resolves+validates, `pruneSettings` strips defaults before saving, and the TUI flyout renders rows straight from the schema. There is no parallel feature map: adding a spec to a segment is the whole job.
- **Config** (`config.go`, `migrate.go`) — TOML at `~/.config/agents-statusline/config.toml` via go-toml/v2; legacy `config.json` migrates automatically (kept as `.bak`, TOML always wins). `validateConfig` normalizes bad values with warnings (shown in `debug`/TUI; stderr only with `STATUSLINE_VERBOSE=1`). Nil-vs-empty `segments` semantics: absent = defaults + plugin auto-append; `[]` = hide all.
- **Themes** (`themes.go`, `depth.go`, `colors.go`) — themes map 15 semantic roles to `themeColor{Hex, Ansi16}` and resolve into the `palette` struct renderers consume; depth (truecolor/256/16/none) detected from env or forced by `color_depth`. The palette carries its theme+depth so `resolveColorSpec` (hex / 256 index / role / legacy name) works wherever a palette flows. **`classic` must stay byte-identical to pre-1.0 output** (`"original"` is an accepted alias for it) — locked by tests.
- **Session state** (`state.go`) — per-session sample history under `$XDG_STATE_HOME/agents-statusline/sessions/`, keyed by `session_id`; powers `cost-rate`, rate-limit projections, and the context trend via the `series` API (Rate/Delta/Span/ProjectWhen/ProjectAt). Segments opt in with `needsState`. Trend features require ≥5min of history (projections: ≥window/4) and hide on flat/falling slopes.
- **Proxy subscription usage** (`internal/foreignusage`) — reads the sanitized Claude/Codex/Grok/Antigravity/Kimi cache. After rendering, `MaybeRefresh` checks the cache mtime against `[foreign_usage].refresh_minutes` (default 1) and, when stale or absent, spawns one detached `foreign-usage-refresh` worker guarded by `foreign-usage.lock`. The worker calls CLIProxyAPI's `/v1/subscription-usage` with the inherited gateway URL/client token (falling back to localhost and `~/.cli-proxy-api/client-key`); provider credentials never leave the proxy. The current render keeps stale data and the next invocation sees the atomic rewrite. Claude model-scoped windows are injected before `state.Record` so projections work. Failures touch an existing cache to prevent retry storms.
- **Plugins** (`internal/plugins`) — executable commands from `[[plugins]]`; single-field (whole stdout) or multi-field (`key:value` lines, one exec per turn). Context via `STATUSLINE_*` env vars. Async plugins read a cache under `$XDG_STATE_HOME/agents-statusline/plugins/` and refresh via a detached hidden `plugin-refresh` subcommand.
- **Release notes** (`releasenotes.go`) — embedded `CHANGELOG.md` (`go:embed`) with optional per-bullet `[N]` importance markers, `release-notes` subcommand (including `vX.Y.Z..vA.B.C` cross-version summaries), and the post-upgrade render-path takeover (`maybeReleaseTakeover` in `runRender`). The takeover sorts bullets by importance, surfaces the highest-importance bullets across the whole upgrade span, and expands up to `[release_notes].max_lines` (default 10; `0` or `"status-line"` keeps the statusline's own height). Window-anchor state at `$XDG_STATE_HOME/agents-statusline/last-version.json`. Settings in the `[release_notes]` config table.
- **Auto-update** (`update.go`) — background check for new releases, `update` segment, `update`/`update verify` subcommands, and detached `update-check` worker. Default mode is `notify` (segment only); `auto` cross-compiles to `brew upgrade agents-statusline` for Homebrew installs or atomic self-swap (`download → verify-sig → sha256-verify → extract → smoke-test → rename`) for manual installs. npm installs are detected (`kindNpm`) and excluded from `auto` self-swap so the binary never fights the package manager; update npm installs with `npm update -g @morgan.rebrand/agents-statusline`. Pi installs use the same npm package and are covered by the same rule; update them with `pi update --extension npm:@morgan.rebrand/agents-statusline` or `pi update`. Cache at `$XDG_STATE_HOME/agents-statusline/update.json`. The render-path trigger is `maybeSpawnUpdateCheck` (one `os.ReadFile`, one detached spawn at most per `check_hours`). `!isReleaseVersion(current)` short-circuits the whole feature (dev, dirty, Go pseudo-versions), mirroring the release-notes carve-out so tests/goldens stay inert.
  - **Signature verification** (`verifyChecksumsSigReal`) authenticates `checksums.txt` against the embedded `cosign.pub` before trusting any digest (fail-closed, pure `crypto/ecdsa`, no runtime cosign). It reads the signature from **either** `messageSignature.signature` (newer sigstore bundle) **or** top-level `base64Signature` (legacy cosign bundle).
  - **Update-outcome confirmation**: the install path writes `update-result.json` (`from`/`to`/`method`/`verified`/`at`); `renderUpdate` shows `✓ updated to vX` for ~5 min when the running version matches `to` (checked before the mode==off guard, so a manual `update` still confirms). `update verify` runs the same signature check on demand and prints `cosignKeyFingerprint()`.
  - **Homebrew tap refresh**: `brew upgrade` runs with `HOMEBREW_NO_AUTO_UPDATE=1`, so `refreshBrewTap` (seam `refreshBrewTapFn`) first `git pull`s our tap's checkout (`brew --repository callmemorgan/tap`) before both brew call sites — otherwise a stale local tap makes brew report "already installed" against an old formula. Best-effort: any failure falls through to the upgrade against whatever's cached.
- **Install** (`install.go`) — settings.json wiring via parse-gated byte splicing (never reformats the user's file; unparseable JSON aborts with a manual snippet); always verifies by piping a sample payload through the configured command.
- **TUI** (`tui.go`, `flyout.go`, `tui_pickers.go`, `tui_colorpicker.go`, `tui_text.go`, `tui_help.go`, `keymap.go`) — single segment-list home screen with floating picker overlays (`tview.Pages`); all selection goes through the `visible` slice + `selectedSegment()`; every mutation goes through `mutate()` (dirty tracking); footer and help generate from the `keymap` table (footers word-wrap via `ansi.FooterRows`). tview/tcell/term stay confined to these files.
- **TUI preview data** — the preview must demonstrate every feature, so it runs on synthetic inputs: `samplePayload()` (carries all payload fields), `previewState()` (an hour of rising session history → cost-rate/projections/trends render), and `gitStatusPreview` (fakes rich git status inside the TUI only — must stay nil on the render path). Demo mode (`d`) sweeps the whole payload via `demoPreviewPayload`; `v` suspends the TUI and renders real escapes to the terminal. Locked by `tui_preview_test.go`.

## Key conventions

- **The bare no-args render path is sacred** — Claude Code invokes the bare binary; subcommands must never change its behavior, and it must never print hints to stdout/stderr.
- **Versioning**: MAJOR.MINOR.REVISION — not strict SemVer. Bump REVISION for bugfixes and features; MINOR for larger milestones.
- **Colors**: always respect `NO_COLOR` and `TERM=dumb` (empty palette). Use `palette` fields or `resolveColorSpec` — never hardcode ANSI codes in renderers. Settings-driven colors must also pass through `resolveColor`, which returns "" when colors are off.
- **Section dividers** use the pattern: `// ─── Section Name ───────────────────────────────────────────────────────────`
- **Commit messages** use [Conventional Commits](https://www.conventionalcommits.org/) with a scope when helpful:
  - `feat(segment): add git-stash segment`
  - `fix(update): refresh the Homebrew tap before brew upgrade`
  - `docs: changelog for v1.3.2`
  - `refactor: tryAcquireLock takes a staleness duration`
  - `chore: ignore .worktrees/`
  
  Use lowercase after the colon, imperative mood, and keep the summary under 72 characters. History before this convention is frozen; do not rewrite it.
- **`CLAUDE.md` is a symlink to this file.** Edit `AGENTS.md`; `CLAUDE.md` follows automatically.

## Publishing

Yes: GitHub releases are published from this repository as a manual maintainer operation. There is intentionally no GitHub Actions release or package-publishing workflow. Before releasing, update both changelog copies, run `make check` and the dead-code analyzer, commit the exact tree, and create a signed annotated `vX.Y.Z` tag. Cross-compile the historical archive-name matrix, inject the bare version/commit/date with ldflags, and attach the archives plus `checksums.txt`, `checksums.txt.sig`, and `checksums.txt.bundle` to the GitHub release. Verify the live assets through `agents-statusline update verify` before announcing completion. Homebrew and npm publishing are separate maintainer operations and are not currently configured for this fork.

## Adding a new built-in segment

1. Write a `renderXxx(ctx renderCtx) (string, bool)` function in `segments.go`.
2. Add an entry to `allSegmentInfos()`: id, natural line (1–9), description, primary color role, optional `settings: []settingSpec` (gives it a flyout automatically), optional `needsState`.
3. Add the segment ID to `defaultConfig()` if it should be on by default (fine when it self-hides without data).
4. Update the segment table in `README.md` and the lists in `help.go`; extend `config.toml.example` if the config schema changed.
5. Add a fixture/assertion: extend a `testdata/payloads/*.json` fixture (regenerate goldens with `-update`) or add a direct renderer test.

## Adding a new built-in theme

1. Add a theme to `builtinThemes` in `themes.go` with a unique id, description, and a colour for each of the 15 semantic roles (`model`, `dir`, `git`, `changes`, `duration`, `cost`, `dim`, `ok`, `warn`, `crit`, `agent`, `vim`, `accent`, `session`, `sep`). Use `themeColor{Hex: "#rrggbb"}` for truecolour/256/16 fallback, or `ansiRole("\x1b[…m")` for an explicit 16-colour-only theme like `classic`.
2. If the theme should emit no colour at all (e.g. `monochrome`), set every role to `ansiRole("")`; `resolvePalette` treats an all-empty theme as disabled so no colour escapes are emitted.
3. Update the theme list in `help.go`, `config.toml.example`, and `README.md`.
4. Add the theme and a canonical background to `THEMES`/`BG` in `scripts/screenshots.py`, then regenerate screenshots with `python3 scripts/screenshots.py`.
5. Run `go test ./...` and smoke-test the theme with a sample payload.

## Homebrew vs local binary

`/opt/homebrew/bin/agents-statusline` is the Homebrew install. `./agents-statusline` in the repo root is the local build. When testing changes, build locally and use `./agents-statusline` directly — and remember the config-migration caution above.
