// Package foreignusage refreshes and reads the sanitized subscription cache
// exposed by CLIProxyAPI. It only handles the proxy client credential; provider
// tokens remain owned by the proxy.
package foreignusage

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/callmemorgan/agents-statusline/internal/config"
	"github.com/callmemorgan/agents-statusline/internal/payload"
	"github.com/callmemorgan/agents-statusline/internal/state"
	"github.com/callmemorgan/agents-statusline/internal/sys"
)

const staleLockTolerance = 30 * time.Second
const refreshTimeout = 15 * time.Second

var refreshHTTPClient = &http.Client{Timeout: refreshTimeout}

type Window struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	UsedPercent float64 `json:"usedPercent"`
	ResetAt     string  `json:"resetAt,omitempty"`
	Scope       string  `json:"scope,omitempty"`
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
// render observes the atomic rewrite performed by the detached worker.
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
	cache, err := fetchProxyUsage()
	if err != nil {
		touchCache()
		return fmt.Errorf("refresh provider usage: %w", err)
	}
	if err := writeCache(cache); err != nil {
		return fmt.Errorf("write provider usage cache: %w", err)
	}
	return nil
}

func fetchProxyUsage() (*Cache, error) {
	endpoint, errEndpoint := proxyUsageEndpoint()
	if errEndpoint != nil {
		return nil, errEndpoint
	}
	token, errToken := proxyToken()
	if errToken != nil {
		return nil, errToken
	}
	req, errRequest := http.NewRequest(http.MethodGet, endpoint, nil)
	if errRequest != nil {
		return nil, errRequest
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, errDo := refreshHTTPClient.Do(req)
	if errDo != nil {
		return nil, errDo
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxy returned HTTP %d", resp.StatusCode)
	}
	var cache Cache
	if errDecode := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&cache); errDecode != nil {
		return nil, errDecode
	}
	if cache.Providers == nil {
		return nil, fmt.Errorf("proxy response has no providers")
	}
	return &cache, nil
}

func proxyUsageEndpoint() (string, error) {
	base := strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL"))
	if base == "" {
		base = "http://127.0.0.1:8317"
	}
	parsed, errParse := url.Parse(base)
	if errParse != nil {
		return "", errParse
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid proxy base URL %q", base)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/subscription-usage"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func proxyToken() (string, error) {
	if token := strings.TrimSpace(os.Getenv("ANTHROPIC_AUTH_TOKEN")); token != "" {
		return token, nil
	}
	home, errHome := os.UserHomeDir()
	if errHome != nil {
		return "", errHome
	}
	path := filepath.Join(home, ".cli-proxy-api", "client-key")
	data, errRead := os.ReadFile(path)
	if errRead != nil {
		return "", fmt.Errorf("read proxy credential %s: %w", path, errRead)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("empty proxy credential: %s", path)
	}
	return token, nil
}

func writeCache(cache *Cache) error {
	if err := os.MkdirAll(state.StateBaseDir(), 0o700); err != nil {
		return err
	}
	data, errMarshal := json.Marshal(cache)
	if errMarshal != nil {
		return errMarshal
	}
	tmp := fmt.Sprintf("%s.%d.tmp", Path(), os.Getpid())
	defer func() { _ = os.Remove(tmp) }()
	if errWrite := os.WriteFile(tmp, data, 0o600); errWrite != nil {
		return errWrite
	}
	if errChmod := os.Chmod(tmp, 0o600); errChmod != nil {
		return errChmod
	}
	return os.Rename(tmp, Path())
}

func touchCache() {
	now := time.Now()
	if _, err := os.Stat(Path()); err == nil {
		_ = os.Chtimes(Path(), now, now)
	}
}

// Load reads the sanitized all-provider cache written by the proxy refresher.
func Load() *Cache {
	data, err := os.ReadFile(Path())
	var cache Cache
	if err == nil {
		_ = json.Unmarshal(data, &cache)
	}
	if len(cache.Providers) == 0 {
		return nil
	}
	return &cache
}

// MaybeInjectClaudeModelScoped fills model-class windows from the proxy cache
// when Claude Code's statusline payload does not provide them itself.
func MaybeInjectClaudeModelScoped(p *payload.Payload, cache *Cache) {
	if p == nil || cache == nil || len(p.RateLimits.ModelScoped) > 0 {
		return
	}
	claude, ok := cache.Providers["claude"]
	if !ok {
		return
	}
	for _, window := range claude.Windows {
		if window.Scope != "model" {
			continue
		}
		percent := window.UsedPercent
		var resetsAt *int64
		if parsed, errParse := time.Parse(time.RFC3339Nano, window.ResetAt); errParse == nil {
			seconds := parsed.Unix()
			resetsAt = &seconds
		}
		p.RateLimits.ModelScoped = append(p.RateLimits.ModelScoped, payload.ModelScopedLimit{
			DisplayName: window.Label, UsedPercentage: &percent, ResetsAt: resetsAt,
		})
	}
}

// RunStatus performs one foreground proxy fetch for manual verification.
func RunStatus(w io.Writer) error {
	cache, err := fetchProxyUsage()
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "proxy subscription usage: %d provider(s)\n", len(cache.Providers))
	for providerName, provider := range cache.Providers {
		if len(provider.Windows) == 0 {
			fmt.Fprintf(w, "%s: %s (%s)\n", providerName, provider.State, provider.Mode)
			continue
		}
		for _, window := range provider.Windows {
			fmt.Fprintf(w, "%s: %s %.0f%% used\n", providerName, window.Label, window.UsedPercent)
		}
	}
	return nil
}
