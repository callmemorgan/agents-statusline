// Package quota implements the OAuth quota shim: a detached worker fetches
// Claude's OAuth usage endpoint (the same one `claude /usage` reads) and
// caches the model-class weekly windows; the render path injects them into
// RateLimits.ModelScoped — a field the statusline wire never populates —
// so the rate-limit-fable/-sonnet/-opus segments render even though Claude
// Code does not send these windows. If Claude Code ever ships them in the
// payload, add wire parsing back in internal/payload against the real field
// names and let it take precedence here. Only percentages and reset times
// are ever written to disk — never the OAuth token.
package quota

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/callmemorgan/agents-statusline/internal/config"
	"github.com/callmemorgan/agents-statusline/internal/payload"
	"github.com/callmemorgan/agents-statusline/internal/state"
	"github.com/callmemorgan/agents-statusline/internal/sys"
)

// ─── Cache ───────────────────────────────────────────────────────────

// usageURL is undocumented but stable in practice; the worker fails soft
// (empty cache, segments hide) if it ever changes shape or disappears.
const usageURL = "https://api.anthropic.com/api/oauth/usage"

const fetchTimeout = 10 * time.Second

// staleLockTolerance must exceed fetchTimeout so a slow fetch is not
// mistaken for a dead worker.
const staleLockTolerance = 30 * time.Second

// cacheEntry is one model-class weekly window, in the same wire shape as
// payload.ModelScopedLimit (resets_at as unix seconds).
type cacheEntry struct {
	DisplayName    string   `json:"display_name"`
	UsedPercentage *float64 `json:"used_percentage,omitempty"`
	ResetsAt       *int64   `json:"resets_at,omitempty"`
}

type cacheFile struct {
	FetchedAt   int64        `json:"fetched_at"`
	ModelScoped []cacheEntry `json:"model_scoped"`
}

func cachePath() string { return filepath.Join(state.StateBaseDir(), "quota-shim.json") }
func lockPath() string  { return filepath.Join(state.StateBaseDir(), "quota-shim.lock") }

// ─── Render-Path Injection ───────────────────────────────────────────

// spawnRefresh starts the detached quota-refresh worker. Tests stub this
// package variable to avoid real process spawning.
var spawnRefresh = spawnRefreshReal

func spawnRefreshReal() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	c := exec.Command(exe, "quota-refresh")
	c.Stdin, c.Stdout, c.Stderr = nil, nil, nil
	sys.ApplyDetachSysProcAttr(c)
	if err := c.Start(); err != nil {
		return err
	}
	return c.Process.Release()
}

// MaybeInject fills p.RateLimits.ModelScoped from the shim cache when the
// shim is enabled and the payload carries no model-class windows of its own,
// and spawns a background refresh when the cache is stale. It never blocks:
// one os.ReadFile when enabled, nothing when disabled.
func MaybeInject(p *payload.Payload, cfg config.QuotaShimConfig, now time.Time) {
	if !cfg.Enabled {
		return
	}
	entries, mtime := loadCache()
	if needsRefresh(mtime, now, cfg.RefreshEvery()) {
		trySpawnRefresh()
	}
	if len(entries) == 0 || len(p.RateLimits.ModelScoped) > 0 {
		return
	}
	scoped := make([]payload.ModelScopedLimit, 0, len(entries))
	for _, e := range entries {
		scoped = append(scoped, payload.ModelScopedLimit{
			DisplayName:    e.DisplayName,
			UsedPercentage: e.UsedPercentage,
			ResetsAt:       e.ResetsAt,
		})
	}
	p.RateLimits.ModelScoped = scoped
}

func loadCache() ([]cacheEntry, time.Time) {
	path := cachePath()
	var mtime time.Time
	if info, err := os.Stat(path); err == nil {
		mtime = info.ModTime()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, mtime
	}
	var f cacheFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, mtime
	}
	return f.ModelScoped, mtime
}

func needsRefresh(mtime time.Time, now time.Time, refresh time.Duration) bool {
	if mtime.IsZero() {
		return true
	}
	return now.Sub(mtime) >= refresh
}

func trySpawnRefresh() {
	if err := os.MkdirAll(state.StateBaseDir(), 0o755); err != nil {
		return
	}
	if sys.TryAcquireLock(lockPath(), staleLockTolerance) {
		if err := spawnRefresh(); err != nil {
			_ = os.Remove(lockPath())
		}
	}
}

// ─── Refresh Worker ──────────────────────────────────────────────────

// RunRefresh executes the hidden "quota-refresh" subcommand: fetch the usage
// endpoint, rewrite the cache atomically, release the lock. Fetch failures
// touch the cache mtime instead so the render path backs off rather than
// respawning every turn.
func RunRefresh() error {
	defer func() { _ = os.Remove(lockPath()) }()
	if err := os.MkdirAll(state.StateBaseDir(), 0o755); err != nil {
		return err
	}

	entries, err := fetchUsage()
	path := cachePath()
	now := time.Now()
	if err != nil {
		if _, statErr := os.Stat(path); statErr == nil {
			_ = os.Chtimes(path, now, now)
		} else {
			_ = os.WriteFile(path, nil, 0o600)
		}
		return nil
	}

	data, marshalErr := json.Marshal(cacheFile{FetchedAt: now.Unix(), ModelScoped: entries})
	if marshalErr != nil {
		return marshalErr
	}
	tmp := path + ".tmp"
	if writeErr := os.WriteFile(tmp, data, 0o600); writeErr == nil {
		_ = os.Rename(tmp, path)
	}
	_ = os.Remove(tmp)
	return nil
}

func fetchUsage() ([]cacheEntry, error) {
	token, err := loadToken()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, usageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	client := &http.Client{Timeout: fetchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("usage endpoint returned " + resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	return parseUsage(body)
}

// ─── Endpoint Parsing ────────────────────────────────────────────────

// oauthWindow is a named weekly window in the usage response. utilization is
// on the 0–100 percent scale (verified against the /usage display).
type oauthWindow struct {
	Utilization *float64        `json:"utilization"`
	ResetsAt    json.RawMessage `json:"resets_at"`
}

// oauthLimit is one entry of the response's limits[] array; weekly_scoped
// entries carry the per-model windows (Fable's included weekly quota).
type oauthLimit struct {
	Kind     string          `json:"kind"`
	Percent  *float64        `json:"percent"`
	ResetsAt json.RawMessage `json:"resets_at"`
	Scope    *struct {
		Model *struct {
			DisplayName string `json:"display_name"`
		} `json:"model"`
	} `json:"scope"`
}

type oauthUsage struct {
	SevenDaySonnet *oauthWindow `json:"seven_day_sonnet"`
	SevenDayOpus   *oauthWindow `json:"seven_day_opus"`
	Limits         []oauthLimit `json:"limits"`
}

// parseUsage extracts the model-class weekly windows: every named
// weekly_scoped limit, plus the top-level seven_day_sonnet/seven_day_opus
// windows when no scoped entry already covers that model class.
func parseUsage(data []byte) ([]cacheEntry, error) {
	var u oauthUsage
	if err := json.Unmarshal(data, &u); err != nil {
		return nil, err
	}
	var entries []cacheEntry
	for _, l := range u.Limits {
		if l.Kind != "weekly_scoped" || l.Percent == nil {
			continue
		}
		if l.Scope == nil || l.Scope.Model == nil || l.Scope.Model.DisplayName == "" {
			continue
		}
		pct := *l.Percent
		entries = append(entries, cacheEntry{
			DisplayName:    l.Scope.Model.DisplayName,
			UsedPercentage: &pct,
			ResetsAt:       payload.ParseResetsAt(l.ResetsAt),
		})
	}
	covered := func(needle string) bool {
		for _, e := range entries {
			if strings.Contains(strings.ToLower(e.DisplayName), needle) {
				return true
			}
		}
		return false
	}
	addWindow := func(w *oauthWindow, name string) {
		if w == nil || w.Utilization == nil || covered(strings.ToLower(name)) {
			return
		}
		pct := *w.Utilization
		entries = append(entries, cacheEntry{
			DisplayName:    name,
			UsedPercentage: &pct,
			ResetsAt:       payload.ParseResetsAt(w.ResetsAt),
		})
	}
	addWindow(u.SevenDaySonnet, "Sonnet")
	addWindow(u.SevenDayOpus, "Opus")
	return entries, nil
}

// ─── Status Subcommand ───────────────────────────────────────────────

// RunStatus implements `agents-statusline quota`: a foreground fetch that
// prints the model-class weekly windows plus shim cache state, so users can
// verify the shim end-to-end before enabling it.
func RunStatus(w io.Writer) error {
	full, _ := config.LoadConfigWarn()
	cfg := full.QuotaShim
	if cfg.Enabled {
		fmt.Fprintf(w, "quota shim: enabled (refresh every %s)\n", cfg.RefreshEvery())
	} else {
		fmt.Fprintln(w, "quota shim: disabled — enable with [quota_shim] enabled = true in config.toml")
	}

	if entries, mtime := loadCache(); !mtime.IsZero() {
		fmt.Fprintf(w, "cache: %s (updated %s ago, %d window(s))\n",
			cachePath(), time.Since(mtime).Round(time.Second), len(entries))
	} else {
		fmt.Fprintf(w, "cache: %s (absent)\n", cachePath())
	}

	entries, err := fetchUsage()
	if err != nil {
		return fmt.Errorf("live fetch failed: %w", err)
	}
	if len(entries) == 0 {
		fmt.Fprintln(w, "live: endpoint reachable, but no model-class weekly windows on this account")
		return nil
	}
	for _, e := range entries {
		line := "live: " + e.DisplayName
		if e.UsedPercentage != nil {
			line += fmt.Sprintf(" %.0f%% used", *e.UsedPercentage)
		}
		if e.ResetsAt != nil {
			line += " (resets " + time.Unix(*e.ResetsAt, 0).Local().Format("2006-01-02 15:04") + ")"
		}
		fmt.Fprintln(w, line)
	}
	return nil
}

// ─── Token Discovery ─────────────────────────────────────────────────

// loadToken finds the Claude Code OAuth access token without ever persisting
// it: $CLAUDE_CODE_OAUTH_TOKEN, then the macOS keychain, then the
// credentials file Claude Code writes on Linux/Windows.
func loadToken() (string, error) {
	if t := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN"); t != "" {
		return t, nil
	}
	if runtime.GOOS == "darwin" {
		if t, err := keychainToken(); err == nil && t != "" {
			return t, nil
		}
	}
	return credentialsFileToken()
}

func keychainToken() (string, error) {
	out, err := exec.Command("security", "find-generic-password",
		"-s", "Claude Code-credentials", "-w").Output()
	if err != nil {
		return "", err
	}
	return accessTokenFromCredentials(out)
}

func credentialsFileToken() (string, error) {
	var candidates []string
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		candidates = append(candidates, filepath.Join(dir, ".credentials.json"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".claude", ".credentials.json"))
	}
	for _, path := range candidates {
		if data, err := os.ReadFile(path); err == nil {
			if t, err := accessTokenFromCredentials(data); err == nil && t != "" {
				return t, nil
			}
		}
	}
	return "", errors.New("no Claude Code OAuth token found (keychain or .credentials.json)")
}

func accessTokenFromCredentials(data []byte) (string, error) {
	var creds struct {
		ClaudeAiOauth struct {
			AccessToken string `json:"accessToken"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", err
	}
	if creds.ClaudeAiOauth.AccessToken == "" {
		return "", errors.New("credentials JSON has no claudeAiOauth.accessToken")
	}
	return creds.ClaudeAiOauth.AccessToken, nil
}
