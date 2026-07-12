package foreignusage

import (
	"os"
	"testing"

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
