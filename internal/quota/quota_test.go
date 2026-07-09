package quota

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/callmemorgan/claude-statusline/internal/config"
	"github.com/callmemorgan/claude-statusline/internal/payload"
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

func writeCache(t *testing.T, entries []cacheEntry) {
	t.Helper()
	data, err := json.Marshal(cacheFile{FetchedAt: time.Now().Unix(), ModelScoped: entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath(), data, 0o600); err != nil {
		t.Fatal(err)
	}
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
		t.Fatalf("payload model_scoped overridden: %+v, want 12%%", got)
	}

	// A payload-provided named window also wins over injected entries.
	var p2 payload.Payload
	named := 33.0
	p2.RateLimits.SevenDayOverageIncluded = payload.LimitWindow{UsedPercentage: &named}
	MaybeInject(&p2, shimEnabled(), time.Now())
	if got := p2.RateLimits.Fable(); got.UsedPercentage == nil || *got.UsedPercentage != 33 {
		t.Fatalf("named window overridden: %+v, want 33%%", got)
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
	if err := os.Chtimes(cachePath(), old, old); err != nil {
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
