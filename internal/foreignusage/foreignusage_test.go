package foreignusage

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/callmemorgan/agents-statusline/internal/config"
	"github.com/callmemorgan/agents-statusline/internal/payload"
	"github.com/callmemorgan/agents-statusline/internal/state"
)

func TestLoadSanitizedCache(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := os.MkdirAll(state.StateBaseDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"providers":{"grok":{"mode":"authoritative","state":"available","windows":[{"id":"weekly","label":"Grok weekly","usedPercent":42}]}}}`)
	if err := os.WriteFile(Path(), data, 0o600); err != nil {
		t.Fatal(err)
	}
	cache := Load()
	if cache == nil || cache.Providers["grok"].Windows[0].UsedPercent != 42 {
		t.Fatalf("cache = %#v", cache)
	}
}

func TestFetchProxyUsageUsesGatewayEnvironment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/subscription-usage" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer proxy-key" {
			t.Fatalf("authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"fetchedAt":"2026-07-19T12:00:00Z","providers":{"claude":{"mode":"authoritative","state":"available","windows":[{"id":"5h","label":"Claude 5h","usedPercent":22}]}}}`))
	}))
	t.Cleanup(server.Close)
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "proxy-key")

	cache, err := fetchProxyUsage()
	if err != nil {
		t.Fatal(err)
	}
	if got := cache.Providers["claude"].Windows[0].UsedPercent; got != 22 {
		t.Fatalf("Claude used percent = %v", got)
	}
}

func TestMaybeRefreshHonorsConfiguredTTL(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := os.MkdirAll(state.StateBaseDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(), []byte(`{"providers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	spawned := 0
	spawnRefresh = func() error { spawned++; return nil }
	t.Cleanup(func() { spawnRefresh = spawnRefreshReal })

	now := time.Now()
	minutes := 3
	cfg := config.ForeignUsageConfig{RefreshMinutes: &minutes}
	MaybeRefresh(cfg, now)
	if spawned != 0 {
		t.Fatalf("fresh cache spawned %d refreshes", spawned)
	}

	old := now.Add(-cfg.RefreshEvery())
	if err := os.Chtimes(Path(), old, old); err != nil {
		t.Fatal(err)
	}
	MaybeRefresh(cfg, now)
	if spawned != 1 {
		t.Fatalf("expired cache spawned %d refreshes, want 1", spawned)
	}
	MaybeRefresh(cfg, now)
	if spawned != 1 {
		t.Fatalf("lock allowed duplicate refresh: %d", spawned)
	}
}

func TestMaybeRefreshMissingCacheSpawns(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	spawned := 0
	spawnRefresh = func() error { spawned++; return nil }
	t.Cleanup(func() { spawnRefresh = spawnRefreshReal })
	MaybeRefresh(config.ForeignUsageConfig{}, time.Now())
	if spawned != 1 {
		t.Fatalf("missing cache spawned %d refreshes, want 1", spawned)
	}
}

func TestMaybeInjectClaudeModelScoped(t *testing.T) {
	cache := &Cache{Providers: map[string]Provider{"claude": {Windows: []Window{
		{ID: "5h", Label: "Claude 5h", UsedPercent: 12},
		{ID: "model-fable", Label: "Fable", UsedPercent: 67, ResetAt: "2026-07-20T12:00:00Z", Scope: "model"},
	}}}}
	var p payload.Payload
	MaybeInjectClaudeModelScoped(&p, cache)
	if len(p.RateLimits.ModelScoped) != 1 {
		t.Fatalf("model scoped = %#v", p.RateLimits.ModelScoped)
	}
	got := p.RateLimits.ModelScoped[0]
	if got.DisplayName != "Fable" || got.UsedPercentage == nil || *got.UsedPercentage != 67 || got.ResetsAt == nil {
		t.Fatalf("Fable = %#v", got)
	}
}

func TestMaybeInjectClaudeModelScopedPayloadWins(t *testing.T) {
	cachePercent, payloadPercent := 67.0, 12.0
	cache := &Cache{Providers: map[string]Provider{"claude": {Windows: []Window{
		{ID: "model-fable", Label: "Fable", UsedPercent: cachePercent, Scope: "model"},
	}}}}
	p := payload.Payload{}
	p.RateLimits.ModelScoped = []payload.ModelScopedLimit{{DisplayName: "Fable", UsedPercentage: &payloadPercent}}
	MaybeInjectClaudeModelScoped(&p, cache)
	if len(p.RateLimits.ModelScoped) != 1 || *p.RateLimits.ModelScoped[0].UsedPercentage != 12 {
		t.Fatalf("payload model scoped overwritten: %#v", p.RateLimits.ModelScoped)
	}
}
