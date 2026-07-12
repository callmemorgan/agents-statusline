# Spec: Auto-update (background check + self-swap)

Status: in progress — handed off for implementation 2026-06-12 (decisions settled by interview)
Target: agents-statusline (this repo), Go, `package main`

## Problem

Releases ship (GoReleaser → GitHub releases + Homebrew tap), but nothing tells
a running install that a new version exists, and manual-tarball users have no
upgrade path short of re-downloading by hand. We want:

1. **Detection**: the statusline learns a newer release exists, without ever
   doing network I/O on the render path.
2. **Notify**: a small `update` segment shows `⬆ v1.2.0` until the user is
   current.
3. **Self-swap (opt-in)**: for non-Homebrew, non-npm installs, download the
   release asset, verify its checksum, and atomically swap the binary in place. The
   next render execs the new binary — and the existing release-notes takeover
   announces what changed. No "apply at next launch" staging step is needed:
   the binary is a one-shot process, so an atomic rename **is** "next launch".
4. A `agents-statusline update` subcommand for explicit, foreground updates.

## Architecture (mirrors existing subsystems — reuse, don't reinvent)

```
render path:  read $XDG_STATE_HOME/agents-statusline/update.json (one ReadFile)
              └ stale? → spawn detached `update-check` (lock + detach, exactly
                          like spawnRefresher in plugins.go) → return immediately

update-check: resolve latest tag (one HTTPS request, no GitHub API quota)
              → write update.json atomically
              → mode == "auto" && newer? → manual install: download, verify, swap
                                         → brew install:   brew upgrade agents-statusline
                                         → npm install:    no-op (npm owns the binary)
```

- The **render path never touches the network**. Its only added cost is one
  small `os.ReadFile` plus, at most once per check interval, one detached
  `exec.Command` spawn — the same budget async plugins already pay.
- The **detached worker** reuses the plugin machinery patterns: lock file with
  the stale-lock recovery of `tryAcquireLock`, atomic tmp+rename cache writes,
  `applyDetachSysProcAttr`. Do not duplicate `tryAcquireLock` — generalize it
  if its plugin-specific timeout handling gets in the way (altitude: shared
  mechanism, not a parallel copy).

## Decisions already made (do not re-litigate)

- **Default mode is `notify`**: daily check + segment, no self-swap. `auto`
  is opt-in. `off` disables everything including the network check.
- **The notify segment self-discloses daily**: while an update is pending,
  the segment renders an **expanded** form for a short window after each
  daily check (`⬆ v1.2.0 · run: agents-statusline update · disable:
  [update] in config.toml`) and a **compact** form (`⬆ v1.2.0`) the rest of
  the day. The window is derived from the cache's `checked_at` — no extra
  state (see segment section).
- **Homebrew installs never self-swap the binary directly** — replacing a
  Cellar-managed binary fights brew's bookkeeping. In `auto` mode the worker
  instead runs **`brew upgrade agents-statusline`** itself (with the rails in
  the worker section); in `notify` mode they get the segment + hint.
- **npm installs never self-swap and never run a package-manager upgrade** —
  npm owns the binary (it lives under `node_modules`), so `auto` is a no-op
  for them: the worker still writes the cache (the notify segment works) but
  takes no install action. Both the segment hint and `agents-statusline
  update` point at **`npm update -g @morgan.rebrand/agents-statusline`**
  instead. Detected by a `node_modules` path component, matched **before**
  brew (a global npm prefix can sit under a Homebrew-managed node). pi
  installs use the same npm package, so they are covered by the same rule;
  update them with `pi update --extension npm:@morgan.rebrand/agents-statusline`
  or `pi update`.
- **`dev` builds disable the whole feature** (no check, no segment, no
  subcommand action beyond an explanatory message). Source builds have no
  comparable version, and this also keeps tests/goldens inert for free —
  the same short-circuit the release-notes feature uses.
- **Auto mode crosses MAJOR versions** — auto means auto; versioning here is
  not strict SemVer and every release is first-party.
- **Downgrades are never applied** (latest < current, e.g. a yanked release
  → do nothing, hide the segment).
- **Default check interval is 24h** (`check_hours`, range 1–168).
- **`agents-statusline install` mentions, never prompts**: its success
  output gains one line — `update checks: notify (configure via [update] in
  config.toml)` — so the default is disclosed at onboarding without making
  install interactive (scripts pipe it).
- **Latest-version lookup avoids the GitHub API** (60 unauthenticated
  req/h/IP): `GET https://github.com/callmemorgan/agents-statusline/releases/latest`
  with redirects disabled → parse the tag from the 302 `Location` header
  (`…/releases/tag/v1.2.0`). Asset URLs are then fully predictable from the
  GoReleaser name template.
- **Integrity = TLS + sha256 against `checksums.txt`** from the same release.
  This protects download integrity; it trusts GitHub. Cosign verification of
  `checksums.txt` would need a sigstore dependency tree — explicitly out of
  scope for v1, noted as future work.
- Repo owner/name are **compile-time constants** (`callmemorgan/agents-statusline`),
  not configurable — a configurable update URL is a foot-gun.

## Config: `[update]` table

```go
// updateConfig is the [update] table in config.toml.
type updateConfig struct {
	Mode       string `toml:"mode,omitempty"`        // "notify" (default) | "auto" | "off"
	CheckHours *int   `toml:"check_hours,omitempty"` // default 24, range 1–168
}

func (u updateConfig) mode() string            // "" → "notify"
func (u updateConfig) checkEvery() time.Duration // nil → 24h
```

`validateConfig` (warn-and-normalize, same pattern as `[release_notes]`):
unknown `mode` → `"%q is not notify/auto/off (using notify)"`, reset to "";
`check_hours` outside 1–168 → `"%d out of range 1-168 (using 24)"`, reset nil.
`mergeWithDefaults` copies `loaded.Update` across (add the merge test).

`config.toml.example`:

```toml
# Update checking: "notify" shows an ⬆ segment when a new release exists,
# "auto" additionally installs it in the background (non-Homebrew, non-npm
# installs), "off" disables checks entirely (no network, ever).
# [update]
# mode = "notify"
# check_hours = 24
```

## New file: `update.go` (+ `update_test.go`)

All logic lives here; one dispatch case in `cmd.go`, one trigger line in
`runRender`, one segment entry in `segments.go`.

### Install-kind detection

```go
type installKind int // kindBrew, kindManual, kindDev, kindNpm

// detectInstallKind classifies the running binary.
//   version == "dev"                            → kindDev
//   eval-symlinked os.Executable() under a path
//     with a "node_modules" component           → kindNpm  (checked first)
//     containing "/Cellar/" or "/homebrew/"     → kindBrew
//   otherwise                                   → kindManual
func detectInstallKind(exePath, version string) installKind
```

`kindNpm` is matched before `kindBrew` because a global npm prefix can live
under a Homebrew-managed node (`/opt/homebrew/lib/node_modules/…`), where the
"homebrew" component precedes "node_modules" — npm owns the file there, so npm
must win. Whole-component match (like the brew heuristic) so `node_modules-backup`
can't false-positive.

Pure on its inputs (pass the resolved path in) so the table test needs no
real symlinks. The caller resolves via `os.Executable()` +
`filepath.EvalSymlinks` — the Homebrew bin symlink points into the Cellar,
which is the reliable signal.

### Version comparison

```go
// compareVersions returns -1/0/+1 for MAJOR.MINOR.REVISION strings
// (leading "v" tolerated). Malformed input compares as equal-to-everything
// (0) so garbage from the network can never trigger a swap.
func compareVersions(a, b string) int
```

Do not import a semver library for three integers.

### Check cache

`$XDG_STATE_HOME/agents-statusline/update.json` (sibling of
`last-version.json`; use `stateBaseDir()`):

```json
{"checked_at": 1718200000, "latest": "1.2.0"}
```

```go
type updateCheck struct {
	CheckedAt int64  `json:"checked_at"`
	Latest    string `json:"latest"` // no leading v; "" = last check failed
}

func loadUpdateCheck() (updateCheck, bool) // ok=false on missing/corrupt
func saveUpdateCheck(c updateCheck) error  // writeFileAtomic
```

Failed checks still write `{now, ""}` so a dead network doesn't respawn the
worker every render — staleness keys off `checked_at`, not success
(the same reasoning as the plugin cache's touch-on-error behavior).

### Render-path trigger

In `runRender`, **after** the print loop, next to `st.Save()` (output is
never delayed):

```go
maybeSpawnUpdateCheck(cfg.Update, start)
```

`maybeSpawnUpdateCheck`: return immediately unless `mode() != "off"`,
`isReleaseVersion(current)`, and `detectInstallKind(...) != kindDev`. Load
the cache; if `now - checked_at < checkEvery()`, return. Otherwise acquire
`update-check.lock` (shared lock helper) and spawn the detached worker:

```go
c := exec.Command(exe, "update-check")
c.Stdin, c.Stdout, c.Stderr = nil, nil, nil
applyDetachSysProcAttr(c)
```

No env-var plumbing needed (unlike plugin-refresh) — the worker re-reads
config itself.

### The detached worker (`update-check`, hidden subcommand)

Hidden: dispatch case in `cmd.go`, **not** listed in `help.go` (same
treatment as `plugin-refresh`). Overall context timeout ~60s; lock released
via defer.

1. Return immediately if `!isReleaseVersion(current)` (dev, dirty, or Go
   pseudo-version) — non-release builds never hit the network.
2. Resolve latest tag (redirect trick above; `http.Client` with
   `CheckRedirect: func(...) error { return http.ErrUseLastResponse }`,
   10s timeout, explicit User-Agent `agents-statusline/<version>`).
3. `saveUpdateCheck({now, latest})` — notify mode stops here, and so does
   `kindNpm` regardless of mode (npm-owned binaries are never self-modified;
   the cache write above is enough for the notify segment to work).
4. Auto mode + `kindBrew` + `compareVersions(latest, current) > 0` →
   run `brew upgrade agents-statusline`:
   - locate `brew` on PATH (also try `/opt/homebrew/bin/brew`,
     `/usr/local/bin/brew`); missing → fall back to notify-only silently.
   - env `HOMEBREW_NO_AUTO_UPDATE=1` (upgrade the formula, don't trigger a
     full `brew update` of every tap from a background process) and
     `HOMEBREW_NO_INSTALL_CLEANUP=1`.
   - own context timeout of 5 minutes (the worker's overall budget rises to
     match on this branch only); stdout/stderr discarded; failure is silent
     — brew's own locks make a concurrent user-run brew safe, and the next
     interval retries.
5. Auto mode, all of: `kindManual`, `compareVersions(latest, current) > 0`,
   exe dir writable → self-swap:
   a. Download `agents-statusline_<Os>_<Arch>.<ext>` from
      `…/releases/download/v<latest>/` into `stateBaseDir()/staging/`.
      Asset name mapping mirrors `.goreleaser.yaml`: GOOS title-cased
      (darwin→`Darwin`), amd64→`x86_64`, windows gets `.zip`, others
      `.tar.gz`. Cap the download at a sane size (64 MiB) — never trust
      Content-Length alone.
   b. Download `checksums.txt`, find the asset's line, verify sha256 of the
      archive bytes. Mismatch → delete staging, abort (cache already
      written, so the segment still notifies).
   c. Extract the `agents-statusline` binary from the archive
      (archive/tar+gzip; archive/zip on Windows), `chmod 0755`.
   d. **Smoke-test**: run the staged binary with `version` (2s timeout) and
      require the output to contain `latest`. A binary that doesn't execute
      on this machine must never reach the exe path.
   e. **Atomic swap** (same-directory renames only — staging may be on a
      different filesystem than the exe):
      1. copy staged → `<exeDir>/.agents-statusline.new`
      2. rename current exe → `<exeDir>/.agents-statusline.old`
      3. rename `.new` → exe path; on failure, rename `.old` back (rollback)
      4. remove `.old`; on Windows this fails while any old process lives —
         ignore, and delete leftover `.old`/`.new` at the start of every
         worker run instead.
   f. Clean the staging dir.

The very next render execs the new binary; `maybeReleaseTakeover` sees the
version change and announces the release notes. The two features compose
with zero new code — that interaction is the point.

Concurrent sessions: the lock serializes workers; renames are atomic;
in-flight execs of the old inode are unaffected on unix. All worker errors
are silent (it's detached — there is nowhere to print).

### The `update` segment

`segments.go` registry entry: id `update`, line 1, description
"Update available notice", primary color role (pick the same role family the
`version` segment uses), no `needsState`, no settings (color override comes
free). Renderer:

```go
func renderUpdate(ctx renderCtx) (string, bool)
```

Hide (`"", false`) unless: mode ≠ off, `isReleaseVersion(current)`,
cache loads, and `compareVersions(cache.Latest, version) > 0`. When shown,
two forms:

- **Expanded** (the daily disclosure) while
  `ctx.Now - checked_at < expandedWindow` (const, **5 minutes**):
  `⬆ v1.2.0 · run: agents-statusline update · disable: [update] in config.toml`
- **Compact** otherwise: `⬆ v1.2.0`

The check runs at most once per `check_hours`, and it only runs while the
user is actively rendering — so the expanded form reliably appears for a few
minutes right after each daily check, then compacts. No new state, no writes
from the render path; `ctx.Now` keeps it deterministic in tests. Reflow
handles the expanded width like any long segment.

Colors through the palette (no hardcoded ANSI); the hint portion uses the
dim role, mirroring the release-notes takeover hint line. Add `update` to
`defaultConfig()` — it self-hides without data, which is the stated bar for
default-on.

### The `update` subcommand (foreground, explicit)

`agents-statusline update` — user-initiated, so it ignores `mode` (explicit
intent) but **not** the safety rails (kind, major, checksum, smoke-test):

- `kindDev` → "source build — update with `go install …@latest`", exit 0.
- `kindNpm` → "installed via npm — update with `npm update -g
  @morgan.rebrand/agents-statusline`", exit 0 (never touches the binary).
- `kindBrew` → run `brew upgrade agents-statusline` in the foreground with
  live output (same rails as the worker's brew branch, minus the silence);
  brew missing → print the manual command, exit 1.
- Already current → "agents-statusline v1.2.0 is up to date", exit 0.
- Newer exists → print what it found, run the same download/verify/swap code
  path as the worker (shared functions, progress on stderr is fine here —
  this is not the render path), report success: "updated v1.1.0 → v1.2.0 —
  run `agents-statusline release-notes` to see what changed".
- `--check` → check + report only, never install. Exit 0 either way.
- Any failure → message on stderr, exit 1.

`--update` flag spelling works for free via the existing
`strings.TrimLeft(os.Args[1], "-")` dispatch.

## What must NOT change

- Goldens and `classic` byte-identity. Test renders run with
  `version == "dev"` → kindDev short-circuits the segment, the spawn, and
  the worker. Verify zero golden churn without `-update`.
- The bare render path: no network, no stdout/stderr output ever (including
  unwritable state dir, dead network, malformed cache), no blocking work
  before the print loop. The spawn happens after printing.
- `install`/`uninstall`/`configure`/`debug` behavior. No TUI work — the
  segment has no settings, so no flyout is needed.
- The release-notes feature: do not touch `last-version.json` or
  `announceDecision` — the takeover composes via the version change alone.

## Tests (`update_test.go`, existing style: tables, `t.TempDir()` + `XDG_STATE_HOME`, fixed clocks)

1. **`detectInstallKind`** — table: Cellar path, /opt/homebrew path, ~/.local/bin,
   dev version (any path), Windows-style path.
2. **`compareVersions`** — orderings, leading-v, malformed → 0;
   property: malformed never compares greater.
3. **Cache round-trip** — save→load equality; corrupt JSON → ok=false;
   failed-check record `{now, ""}` loads and suppresses respawn.
4. **`maybeSpawnUpdateCheck`** — stub the spawn (package var, like
   `spawnRefresher`): off-mode never spawns; fresh cache never spawns; stale
   cache spawns once and lock blocks the second; dev / non-release version
   (`+dirty`, Go pseudo-version) never spawns.
5. **Asset naming** — table mapping GOOS/GOARCH → exact GoReleaser asset
   filename (locks the template contract; a rename in `.goreleaser.yaml`
   must fail this test).
6. **Checksum verify** — known bytes + real checksums.txt line → pass;
   flipped bit → fail.
7. **Swap** — in `t.TempDir()`: fake "exe" file, staged replacement → swap
   succeeds, content is the new binary, `.old` removed; failure injection at
   step 3 → rollback leaves the original intact and `.new`/`.old` cleaned.
8. **`renderUpdate`** — hides on: no cache, equal version, older latest,
   mode=off, non-release version; expanded form within 5min of `checked_at`,
   compact after (fixed `ctx.Now`); empty palette → no `\x1b` bytes in either
   form.
9. **Brew branch** — stub the exec (package var): auto+brew runs the upgrade
   command with `HOMEBREW_NO_AUTO_UPDATE=1`; brew missing falls back
   silently; notify+brew never execs.
10. **Config** — `[update]` round-trips; `mergeWithDefaults` preserves it;
    bad mode / out-of-range hours warn and normalize; defaults
    (`mode()=="notify"`, 24h).
11. **Goldens** — `go test ./...` green, zero golden changes.

Network and the real swap-into-`os.Executable()` path are covered by the
manual smoke below, not unit tests — do not mock HTTP for coverage theater.

## Documentation updates (required, same PR)

1. `help.go` — Commands block: `update  Check for a new release and install
   it (--check: report only).` Configuration block: `[update]` one-liner
   (`mode: notify/auto/off, check_hours`). `update-check` stays unlisted.
2. `README.md` — command table row; segment table row for `update`; a short
   "Updates" section: notify default, the daily expanded disclosure, auto
   opt-in (self-swap for manual installs, `brew upgrade` for brew installs),
   the no-network-on-render guarantee, `mode = "off"` for air-gapped/teams.
3. `install.go` — append the one-line mention to the success output:
   `update checks: notify (configure via [update] in config.toml)`.
4. `config.toml.example` — the commented block above.
5. `CLAUDE.md` — key-subsystems bullet for `update.go`; note the sacred-path
   carve-out (post-print spawn, cache read). **Copy CLAUDE.md over AGENTS.md.**

## Implementation order

1. Config table + validation + merge (+ test 9).
2. `detectInstallKind`, `compareVersions`, cache load/save (+ tests 1–3).
3. Lock generalization (if needed) + `maybeSpawnUpdateCheck` + runRender
   trigger (+ test 4).
4. Worker: tag resolution, asset naming, download, checksum, extract,
   smoke-test, swap (+ tests 5–7); `update-check` dispatch case.
5. `update` segment + defaultConfig (+ test 8).
6. `update` subcommand + dispatch (+ docs).
7. `go test ./...` + manual smoke (isolated env, **and an isolated copy of
   the binary** — never point the swap at the repo build you're editing):

   ```bash
   go build -ldflags "-X github.com/callmemorgan/agents-statusline/internal/version.Version=1.0.0" -o /tmp/upd-test/bin/agents-statusline ./cmd/agents-statusline
   export HOME=/tmp/upd-test XDG_STATE_HOME=/tmp/upd-test/state XDG_CONFIG_HOME=/tmp/upd-test/config
   printf '[update]\nmode = "auto"\n' > …/config/agents-statusline/config.toml
   echo '{"model":{"display_name":"Claude"},"workspace":{"current_dir":"~"}}' | /tmp/upd-test/bin/agents-statusline
   # wait for the worker; binary should now be the latest release, and the
   # next render shows the release-notes takeover
   /tmp/upd-test/bin/agents-statusline version
   /tmp/upd-test/bin/agents-statusline update --check
   ```

## Acceptance criteria

- [ ] `go test ./...` passes; zero golden changes.
- [ ] Render path does no network I/O and prints nothing extra under any
      failure (verified by reading the code path, plus dead-network smoke).
- [ ] `mode = "off"` produces zero spawns and zero reads beyond the config.
- [ ] Notify mode: `⬆ v<latest>` segment appears when behind (expanded for
      ~5min after each daily check, compact otherwise), disappears when
      current; never appears for dev/current/downgrade cases.
- [ ] Auto mode on a manual install: binary is replaced within one check
      interval; next render announces via the release-notes takeover;
      checksum mismatch or failed smoke-test leaves the old binary untouched.
- [ ] Auto mode on a Homebrew install: never touches the binary directly;
      runs `brew upgrade agents-statusline` with `HOMEBREW_NO_AUTO_UPDATE=1`,
      silently skipping when brew is absent.
- [ ] `agents-statusline install` output mentions the update-check default.
- [ ] `agents-statusline update` / `--check` behave per the table; honest
      exit codes.
- [ ] Interrupted/concurrent workers never corrupt the exe (lock + rename
      rollback; `.old`/`.new` leftovers cleaned on the next run).
- [ ] README, help.go, install output, config.toml.example, CLAUDE.md +
      AGENTS.md updated (last two identical).

## Interview record (2026-06-12)

- Default mode **notify**, with the daily expanded-segment disclosure and
  disable instructions. ✔
- `update` segment **default-on**. ✔
- Integrity: **checksums only** (TLS + sha256); cosign verification is
  future work. ✔
- Auto mode **crosses MAJOR versions** (no fence). ✔
- Auto mode on brew installs **runs `brew upgrade` itself**. ✔
- `install` **mentions** the default, never prompts. ✔
- Check interval default **24h**. ✔
