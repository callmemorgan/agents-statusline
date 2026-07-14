package foreignusage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/callmemorgan/agents-statusline/internal/config"
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

func TestEnvironmentWithRefreshPathAddsHomebrewNodeLocations(t *testing.T) {
	got := environmentWithRefreshPath([]string{"HOME=/tmp/home", "PATH=/usr/bin:/bin"})
	var path string
	for _, entry := range got {
		if strings.HasPrefix(entry, "PATH=") {
			path = entry
		}
	}
	if !strings.HasPrefix(path, "PATH=/opt/homebrew/bin:/usr/local/bin:") {
		t.Fatalf("PATH = %q", path)
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

func TestLoadMergesClaudeQuotaCache(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := os.MkdirAll(state.StateBaseDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"five_hour":{"display_name":"Claude 5h","used_percentage":32,"resets_at":1783900800},"seven_day":{"display_name":"Claude weekly","used_percentage":18}}`)
	if err := os.WriteFile(filepath.Join(state.StateBaseDir(), "quota-shim.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	cache := Load()
	if cache == nil {
		t.Fatal("Load() = nil")
	}
	claude := cache.Providers["claude"]
	if claude.Mode != "authoritative" || len(claude.Windows) != 2 {
		t.Fatalf("claude = %#v", claude)
	}
	if claude.Windows[0].Label != "Claude 5h" || claude.Windows[0].UsedPercent != 32 {
		t.Fatalf("five-hour window = %#v", claude.Windows[0])
	}
	if claude.Windows[1].Label != "Claude weekly" || claude.Windows[1].UsedPercent != 18 {
		t.Fatalf("weekly window = %#v", claude.Windows[1])
	}
}
