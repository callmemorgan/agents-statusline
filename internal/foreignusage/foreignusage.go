// Package foreignusage reads the sanitized subscription cache written by
// claude-all-usage. It never performs network I/O or reads provider tokens.
package foreignusage

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/callmemorgan/agents-statusline/internal/state"
)

type Window struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	UsedPercent float64 `json:"usedPercent"`
	ResetAt     string  `json:"resetAt,omitempty"`
}

type Provider struct {
	Mode       string   `json:"mode"`
	State      string   `json:"state"`
	CooldownAt string   `json:"cooldownAt,omitempty"`
	Windows    []Window `json:"windows"`
}

type Cache struct {
	FetchedAt string              `json:"fetchedAt"`
	Providers map[string]Provider `json:"providers"`
}

func Path() string { return filepath.Join(state.StateBaseDir(), "foreign-usage.json") }

func Load() *Cache {
	data, err := os.ReadFile(Path())
	if err != nil {
		return nil
	}
	var cache Cache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil
	}
	return &cache
}
