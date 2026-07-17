package quota

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/callmemorgan/agents-statusline/internal/config"
	"github.com/callmemorgan/agents-statusline/internal/payload"
)

// usageFixture is an anonymized copy of a real /api/oauth/usage response
// (2026-07-09): Fable arrives as a weekly_scoped limit, Sonnet/Opus as
// top-level windows that are null on Max plans.
const usageFixture = `{
  "five_hour": {"utilization": 2.0, "resets_at": "2026-07-09T23:00:00.080277+00:00"},
  "seven_day": {"utilization": 0.0, "resets_at": "2026-07-11T16:00:00.080304+00:00"},
  "seven_day_opus": null,
  "seven_day_sonnet": {"utilization": 41.5, "resets_at": "2026-07-11T16:00:00+00:00"},
  "limits": [
    {"kind": "session", "group": "session", "percent": 2, "resets_at": "2026-07-09T23:00:00.080277+00:00", "scope": null},
    {"kind": "weekly_all", "group": "weekly", "percent": 0, "resets_at": "2026-07-11T16:00:00.080304+00:00", "scope": null},
    {"kind": "weekly_scoped", "group": "weekly", "percent": 67, "resets_at": "2026-07-11T16:00:00+00:00",
     "scope": {"model": {"id": null, "display_name": "Fable"}, "surface": null}}
  ]
}`

func TestParseUsage(t *testing.T) {
	entries, err := parseUsage([]byte(usageFixture))
	if err != nil {
		t.Fatalf("parseUsage: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2 (Fable scoped + Sonnet window): %+v", len(entries), entries)
	}
	fable := entries[0]
	if fable.DisplayName != "Fable" {
		t.Errorf("entry 0 display_name = %q, want Fable", fable.DisplayName)
	}
	if fable.UsedPercentage == nil || *fable.UsedPercentage != 67 {
		t.Errorf("Fable used_percentage = %v, want 67", fable.UsedPercentage)
	}
	if fable.ResetsAt == nil {
		t.Errorf("Fable resets_at = nil, want parsed RFC3339")
	} else if got := time.Unix(*fable.ResetsAt, 0).UTC().Format("2006-01-02T15:04"); got != "2026-07-11T16:00" {
		t.Errorf("Fable resets_at = %s", got)
	}
	sonnet := entries[1]
	if sonnet.DisplayName != "Sonnet" || sonnet.UsedPercentage == nil || *sonnet.UsedPercentage != 41.5 {
		t.Errorf("entry 1 = %+v, want Sonnet 41.5", sonnet)
	}
}

func TestParseUsageCacheIncludesAccountWindows(t *testing.T) {
	cache, err := parseUsageCache([]byte(usageFixture))
	if err != nil {
		t.Fatal(err)
	}
	if cache.FiveHour == nil || cache.FiveHour.UsedPercentage == nil || *cache.FiveHour.UsedPercentage != 2 {
		t.Fatalf("five_hour = %+v, want 2%%", cache.FiveHour)
	}
	if cache.SevenDay == nil || cache.SevenDay.UsedPercentage == nil || *cache.SevenDay.UsedPercentage != 0 {
		t.Fatalf("seven_day = %+v, want 0%%", cache.SevenDay)
	}
	if cache.FiveHour.DisplayName != "Claude 5h" || cache.SevenDay.DisplayName != "Claude weekly" {
		t.Fatalf("labels = %q, %q", cache.FiveHour.DisplayName, cache.SevenDay.DisplayName)
	}
}

func TestParseUsageNullResets(t *testing.T) {
	entries, err := parseUsage([]byte(`{"limits":[{"kind":"weekly_scoped","percent":0,"resets_at":null,"scope":{"model":{"display_name":"Fable"}}}]}`))
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	if entries[0].ResetsAt != nil {
		t.Errorf("resets_at = %v, want nil", *entries[0].ResetsAt)
	}
}

// TestParseUsageScopedWins checks a scoped Sonnet entry suppresses the
// top-level seven_day_sonnet duplicate.
func TestParseUsageScopedWins(t *testing.T) {
	entries, err := parseUsage([]byte(`{
	  "seven_day_sonnet": {"utilization": 10},
	  "limits":[{"kind":"weekly_scoped","percent":12,"scope":{"model":{"display_name":"Sonnet 5"}}}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || *entries[0].UsedPercentage != 12 {
		t.Fatalf("entries = %+v, want single scoped Sonnet at 12%%", entries)
	}
}

func writeCacheAt(t *testing.T, path string, entries []cacheEntry) {
	t.Helper()
	data, err := json.Marshal(cacheFile{FetchedAt: time.Now().Unix(), ModelScoped: entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// writeCache writes the default profile's cache; tests calling it also clear
// CLAUDE_CONFIG_DIR so a developer's own profile env can't skew the key.
func writeCache(t *testing.T, entries []cacheEntry) {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	writeCacheAt(t, cachePath(""), entries)
}

func shimEnabled() config.QuotaShimConfig {
	return config.QuotaShimConfig{Enabled: true}
}

func TestMaybeInjectFillsModelScoped(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	spawned := 0
	spawnRefresh = func() error { spawned++; return nil }
	t.Cleanup(func() { spawnRefresh = spawnRefreshReal })

	pct := 67.0
	writeCache(t, []cacheEntry{{DisplayName: "Fable", UsedPercentage: &pct}})

	var p payload.Payload
	MaybeInject(&p, shimEnabled(), time.Now())

	got := p.RateLimits.Fable()
	if got.UsedPercentage == nil || *got.UsedPercentage != 67 {
		t.Fatalf("Fable() = %+v, want 67%%", got)
	}
	if spawned != 0 {
		t.Errorf("fresh cache spawned %d refreshes, want 0", spawned)
	}
}

func TestMaybeInjectDisabled(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	spawnRefresh = func() error { t.Error("spawned while disabled"); return nil }
	t.Cleanup(func() { spawnRefresh = spawnRefreshReal })

	pct := 67.0
	writeCache(t, []cacheEntry{{DisplayName: "Fable", UsedPercentage: &pct}})

	var p payload.Payload
	MaybeInject(&p, config.QuotaShimConfig{}, time.Now())
	if len(p.RateLimits.ModelScoped) != 0 {
		t.Error("disabled shim injected data")
	}
}

func TestMaybeInjectPayloadWins(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	spawnRefresh = func() error { return nil }
	t.Cleanup(func() { spawnRefresh = spawnRefreshReal })

	shimPct := 67.0
	writeCache(t, []cacheEntry{{DisplayName: "Fable", UsedPercentage: &shimPct}})

	realPct := 12.0
	var p payload.Payload
	p.RateLimits.ModelScoped = []payload.ModelScopedLimit{{DisplayName: "Fable", UsedPercentage: &realPct}}
	MaybeInject(&p, shimEnabled(), time.Now())

	if got := p.RateLimits.Fable(); got.UsedPercentage == nil || *got.UsedPercentage != 12 {
		t.Fatalf("pre-populated model_scoped overridden: %+v, want 12%%", got)
	}
}

func TestMaybeInjectStaleCacheSpawnsRefresh(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	spawned := 0
	spawnRefresh = func() error { spawned++; return nil }
	t.Cleanup(func() { spawnRefresh = spawnRefreshReal })

	pct := 67.0
	writeCache(t, []cacheEntry{{DisplayName: "Fable", UsedPercentage: &pct}})
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(cachePath(""), old, old); err != nil {
		t.Fatal(err)
	}

	var p payload.Payload
	MaybeInject(&p, shimEnabled(), time.Now())
	if spawned != 1 {
		t.Errorf("stale cache spawned %d refreshes, want 1", spawned)
	}
	// Stale data still renders while the refresh runs in the background.
	if got := p.RateLimits.Fable(); got.UsedPercentage == nil {
		t.Error("stale cache not injected")
	}

	// Second render inside the lock window must not spawn again.
	MaybeInject(&payload.Payload{}, shimEnabled(), time.Now())
	if spawned != 1 {
		t.Errorf("lock did not serialize refreshes: %d spawns", spawned)
	}
}

func TestMaybeInjectMissingCacheSpawnsButInjectsNothing(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	spawned := 0
	spawnRefresh = func() error { spawned++; return nil }
	t.Cleanup(func() { spawnRefresh = spawnRefreshReal })

	var p payload.Payload
	MaybeInject(&p, shimEnabled(), time.Now())
	if len(p.RateLimits.ModelScoped) != 0 {
		t.Error("injected entries with no cache")
	}
	if spawned != 1 {
		t.Errorf("missing cache spawned %d refreshes, want 1", spawned)
	}
}

func TestAccessTokenFromCredentials(t *testing.T) {
	tok, err := accessTokenFromCredentials([]byte(`{"claudeAiOauth":{"accessToken":"sk-ant-oat01-xyz"}}`))
	if err != nil || tok != "sk-ant-oat01-xyz" {
		t.Fatalf("tok=%q err=%v", tok, err)
	}
	if _, err := accessTokenFromCredentials([]byte(`{}`)); err == nil {
		t.Error("empty credentials should error")
	}
}

func TestKeychainServiceName(t *testing.T) {
	if got := keychainServiceName(""); got != "Claude Code-credentials" {
		t.Errorf("default profile service = %q, want unsuffixed", got)
	}
	// sha256("/Users/morgan/.claude-personal")[:8] == "ef26ec72", verified
	// against a real Claude Code-managed keychain item on the host that
	// exports CLAUDE_CONFIG_DIR="$HOME/.claude-personal" for that profile.
	const want = "Claude Code-credentials-ef26ec72"
	if got := keychainServiceName("/Users/morgan/.claude-personal"); got != want {
		t.Errorf("scoped profile service = %q, want %q", got, want)
	}
}

func TestResolveClaudeConfigDir(t *testing.T) {
	t.Run("unset falls back to default profile", func(t *testing.T) {
		t.Setenv("CLAUDE_CONFIG_DIR", "")
		if got := resolveClaudeConfigDir(config.QuotaShimConfig{}); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("env is used when config value is unset", func(t *testing.T) {
		t.Setenv("CLAUDE_CONFIG_DIR", "/tmp/env-profile")
		if got := resolveClaudeConfigDir(config.QuotaShimConfig{}); got != "/tmp/env-profile" {
			t.Errorf("got %q, want env value", got)
		}
	})

	t.Run("explicit config wins over env", func(t *testing.T) {
		t.Setenv("CLAUDE_CONFIG_DIR", "/tmp/env-profile")
		cfg := config.QuotaShimConfig{ClaudeConfigDir: "/tmp/configured-profile"}
		if got := resolveClaudeConfigDir(cfg); got != "/tmp/configured-profile" {
			t.Errorf("got %q, want configured value", got)
		}
	})

	t.Run("leading tilde expands against home", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		cfg := config.QuotaShimConfig{ClaudeConfigDir: "~/.claude-personal"}
		want := filepath.Join(home, ".claude-personal")
		if got := resolveClaudeConfigDir(cfg); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestProfileCacheKeying(t *testing.T) {
	// Default profile keeps the legacy filenames for backward compat.
	if got := filepath.Base(cachePath("")); got != "quota-shim.json" {
		t.Errorf("default cache = %q, want quota-shim.json", got)
	}
	if got := filepath.Base(lockPath("")); got != "quota-shim.lock" {
		t.Errorf("default lock = %q, want quota-shim.lock", got)
	}
	// A scoped profile appends the same 8-hex suffix as the keychain service
	// (sha256("/Users/morgan/.claude-personal")[:8] == "ef26ec72").
	const dir = "/Users/morgan/.claude-personal"
	if got := filepath.Base(cachePath(dir)); got != "quota-shim-ef26ec72.json" {
		t.Errorf("scoped cache = %q, want quota-shim-ef26ec72.json", got)
	}
	if got := filepath.Base(lockPath(dir)); got != "quota-shim-ef26ec72.lock" {
		t.Errorf("scoped lock = %q, want quota-shim-ef26ec72.lock", got)
	}
	// The cache/lock suffix and the keychain suffix must never diverge.
	wantSuffix := "-" + keychainServiceName(dir)[len("Claude Code-credentials-"):]
	if got := profileSuffix(dir); got != wantSuffix {
		t.Errorf("profileSuffix = %q, keychain suffix = %q", got, wantSuffix)
	}
}

func TestCachePathResolvesProfileInProcess(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "/Users/morgan/.claude-personal")
	if got := filepath.Base(CachePath(config.QuotaShimConfig{})); got != "quota-shim-ef26ec72.json" {
		t.Errorf("env-scoped CachePath = %q, want quota-shim-ef26ec72.json", got)
	}
	// Config beats env for the cache key too, matching token discovery.
	cfg := config.QuotaShimConfig{ClaudeConfigDir: "/tmp/other-profile"}
	if got := CachePath(cfg); got != cachePath("/tmp/other-profile") {
		t.Errorf("configured CachePath = %q, want %q", got, cachePath("/tmp/other-profile"))
	}
}

func TestMaybeInjectReadsProfileScopedCache(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	spawnRefresh = func() error { return nil }
	t.Cleanup(func() { spawnRefresh = spawnRefreshReal })

	const profile = "/tmp/scoped-profile"
	defaultPct, scopedPct := 11.0, 67.0
	writeCacheAt(t, cachePath(""), []cacheEntry{{DisplayName: "Fable", UsedPercentage: &defaultPct}})
	writeCacheAt(t, cachePath(profile), []cacheEntry{{DisplayName: "Fable", UsedPercentage: &scopedPct}})

	var p payload.Payload
	MaybeInject(&p, config.QuotaShimConfig{Enabled: true, ClaudeConfigDir: profile}, time.Now())
	if got := p.RateLimits.Fable(); got.UsedPercentage == nil || *got.UsedPercentage != 67 {
		t.Fatalf("scoped profile injected %+v, want the scoped cache's 67%%", got)
	}

	var q payload.Payload
	MaybeInject(&q, config.QuotaShimConfig{Enabled: true}, time.Now())
	if got := q.RateLimits.Fable(); got.UsedPercentage == nil || *got.UsedPercentage != 11 {
		t.Fatalf("default profile injected %+v, want the default cache's 11%%", got)
	}
}

// TestMaybeInjectStaleScopedCacheLocksScopedPath checks the spawn path keys
// the lock by profile: a stale scoped cache must acquire the profile-scoped
// lock, not the default quota-shim.lock, before spawning the worker.
func TestMaybeInjectStaleScopedCacheLocksScopedPath(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	spawned := 0
	spawnRefresh = func() error { spawned++; return nil }
	t.Cleanup(func() { spawnRefresh = spawnRefreshReal })

	const profile = "/tmp/scoped-profile"
	pct := 67.0
	writeCacheAt(t, cachePath(profile), []cacheEntry{{DisplayName: "Fable", UsedPercentage: &pct}})
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(cachePath(profile), old, old); err != nil {
		t.Fatal(err)
	}

	var p payload.Payload
	MaybeInject(&p, config.QuotaShimConfig{Enabled: true, ClaudeConfigDir: profile}, time.Now())
	if spawned != 1 {
		t.Errorf("stale scoped cache spawned %d refreshes, want 1", spawned)
	}
	if _, err := os.Stat(lockPath(profile)); err != nil {
		t.Errorf("profile-scoped lock not acquired: %v", err)
	}
	if _, err := os.Stat(lockPath("")); !os.IsNotExist(err) {
		t.Error("spawn took the default profile's lock")
	}
}

// TestRunRefreshUsesProfileScopedPaths runs the real worker entry point with
// CLAUDE_CONFIG_DIR inherited via env (exactly how the spawned process gets
// it) and no reachable token, and checks the failure path lands on the
// profile-scoped cache and releases the profile-scoped lock — proving the
// worker re-derives the same key as the render path that spawned it.
func TestRunRefreshUsesProfileScopedPaths(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	config.ConfigDirOverride = t.TempDir() // empty config dir -> defaults
	t.Cleanup(func() { config.ConfigDirOverride = "" })
	profile := filepath.Join(t.TempDir(), "scoped-profile") // no keychain entry, no .credentials.json
	t.Setenv("CLAUDE_CONFIG_DIR", profile)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("HOME", t.TempDir()) // hide any real ~/.claude/.credentials.json

	if err := os.MkdirAll(filepath.Dir(lockPath(profile)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath(profile), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunRefresh(); err != nil {
		t.Fatalf("RunRefresh: %v", err)
	}
	if _, err := os.Stat(lockPath(profile)); !os.IsNotExist(err) {
		t.Error("profile-scoped lock not released")
	}
	if _, err := os.Stat(cachePath(profile)); err != nil {
		t.Errorf("fetch failure should write the profile-scoped cache: %v", err)
	}
	if _, err := os.Stat(cachePath("")); !os.IsNotExist(err) {
		t.Error("worker touched the default profile's cache")
	}
}
