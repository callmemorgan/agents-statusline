// Package foreignusage reads the sanitized subscription cache written by
// claude-all-usage. It never performs network I/O or reads provider tokens.
package foreignusage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/callmemorgan/agents-statusline/internal/config"
	"github.com/callmemorgan/agents-statusline/internal/state"
	"github.com/callmemorgan/agents-statusline/internal/sys"
)

const staleLockTolerance = 30 * time.Second

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

func Path() string     { return filepath.Join(state.StateBaseDir(), "foreign-usage.json") }
func lockPath() string { return filepath.Join(state.StateBaseDir(), "foreign-usage.lock") }

var spawnRefresh = spawnRefreshReal

func spawnRefreshReal() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "foreign-usage-refresh")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	sys.ApplyDetachSysProcAttr(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// MaybeRefresh starts one detached refresh when the cache is older than the
// configured cadence. The current render keeps using the cached value; a later
// render observes the atomic rewrite performed by claude-all-usage.
func MaybeRefresh(cfg config.ForeignUsageConfig, now time.Time) {
	info, err := os.Stat(Path())
	if err == nil && now.Sub(info.ModTime()) < cfg.RefreshEvery() {
		return
	}
	if err := os.MkdirAll(state.StateBaseDir(), 0o755); err != nil {
		return
	}
	if !sys.TryAcquireLock(lockPath(), staleLockTolerance) {
		return
	}
	if err := spawnRefresh(); err != nil {
		_ = os.Remove(lockPath())
	}
}

// RunRefresh implements the hidden foreign-usage-refresh worker.
func RunRefresh() error {
	defer func() { _ = os.Remove(lockPath()) }()
	command, err := refreshCommand()
	if err != nil {
		touchCache()
		return err
	}
	cmd := exec.Command(command)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	cmd.Env = environmentWithRefreshPath(os.Environ())
	if err := cmd.Run(); err != nil {
		touchCache()
		return fmt.Errorf("refresh provider usage: %w", err)
	}
	return nil
}

func environmentWithRefreshPath(environment []string) []string {
	prefix := strings.Join([]string{"/opt/homebrew/bin", "/usr/local/bin"}, string(os.PathListSeparator))
	for index, entry := range environment {
		if strings.HasPrefix(strings.ToUpper(entry), "PATH=") {
			copy := append([]string(nil), environment...)
			copy[index] = "PATH=" + prefix + string(os.PathListSeparator) + entry[strings.IndexByte(entry, '=')+1:]
			return copy
		}
	}
	return append(append([]string(nil), environment...), "PATH="+prefix)
}

func refreshCommand() (string, error) {
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "claude-all-usage")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	if command, err := exec.LookPath("claude-all-usage"); err == nil {
		return command, nil
	}
	return "", errors.New("claude-all-usage not found next to agents-statusline or on PATH")
}

func touchCache() {
	now := time.Now()
	if _, err := os.Stat(Path()); err == nil {
		_ = os.Chtimes(Path(), now, now)
	}
}

func Load() *Cache {
	data, err := os.ReadFile(Path())
	var cache Cache
	if err == nil {
		_ = json.Unmarshal(data, &cache)
	}
	if cache.Providers == nil {
		cache.Providers = make(map[string]Provider)
	}
	if claude, ok := loadClaudeUsage(); ok {
		cache.Providers["claude"] = claude
	}
	if len(cache.Providers) == 0 {
		return nil
	}
	return &cache
}

type claudeCacheEntry struct {
	DisplayName    string   `json:"display_name"`
	UsedPercentage *float64 `json:"used_percentage"`
	ResetsAt       *int64   `json:"resets_at"`
}

type claudeCache struct {
	FiveHour *claudeCacheEntry `json:"five_hour"`
	SevenDay *claudeCacheEntry `json:"seven_day"`
}

func loadClaudeUsage() (Provider, bool) {
	data, err := os.ReadFile(filepath.Join(state.StateBaseDir(), "quota-shim.json"))
	if err != nil {
		return Provider{}, false
	}
	var cache claudeCache
	if json.Unmarshal(data, &cache) != nil {
		return Provider{}, false
	}
	windows := make([]Window, 0, 2)
	appendWindow := func(id, fallback string, entry *claudeCacheEntry) {
		if entry == nil || entry.UsedPercentage == nil {
			return
		}
		label := entry.DisplayName
		if label == "" {
			label = fallback
		}
		reset := ""
		if entry.ResetsAt != nil {
			reset = time.Unix(*entry.ResetsAt, 0).UTC().Format(time.RFC3339)
		}
		windows = append(windows, Window{ID: id, Label: label, UsedPercent: *entry.UsedPercentage, ResetAt: reset})
	}
	appendWindow("5h", "Claude 5h", cache.FiveHour)
	appendWindow("weekly", "Claude weekly", cache.SevenDay)
	if len(windows) == 0 {
		return Provider{}, false
	}
	return Provider{Mode: "authoritative", State: "available", Windows: windows}, true
}
