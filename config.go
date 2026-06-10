package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ─── Config ──────────────────────────────────────────────────────────

type pluginField struct {
	ID   string `json:"id"`
	Line int    `json:"line"`
	Desc string `json:"desc"`
}

type pluginDef struct {
	ID        string        `json:"id"`
	Command   string        `json:"command"`
	Line      int           `json:"line"`
	Desc      string        `json:"desc"`
	TimeoutMS int           `json:"timeout_ms"`
	Fields    []pluginField `json:"fields"`
}

type config struct {
	Segments []string                  `json:"segments"`
	Lines    map[string]int            `json:"lines"`
	Colors   map[string]string         `json:"colors"`
	Plugins  []pluginDef               `json:"plugins"`
	Reflow   string                    `json:"reflow"`
	Settings map[string]map[string]any `json:"settings"`
}

func defaultConfig() config {
	return config{
		Segments: []string{
			"vim-mode", "sandbox", "session-name", "agent-state", "directory",
			"git-branch", "artifact-count", "lines-changed", "cache-percent", "cost",
			"model", "version", "duration", "api-efficiency", "tokens",
			"context-window", "rate-limit-5h", "rate-limit-7d", "plan-tier",
		},
		Lines: nil,
	}
}

// configDirOverride redirects the config directory; set only by tests.
var configDirOverride string

func configDir() string {
	if configDirOverride != "" {
		return configDirOverride
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "~"
	}
	return filepath.Join(home, ".config", "claude-statusline")
}

func configPath() string {
	return filepath.Join(configDir(), "config.json")
}

func loadConfig() config {
	cfg := defaultConfig()
	data, err := os.ReadFile(configPath())
	if err != nil {
		return cfg
	}
	var loaded config
	if err := json.Unmarshal(data, &loaded); err != nil {
		return cfg
	}
	// An explicit empty array means "hide everything"; only fall back to
	// defaults when the key is absent entirely (nil vs []).
	if loaded.Segments != nil {
		cfg.Segments = loaded.Segments
	}
	cfg.Lines = loaded.Lines
	cfg.Colors = loaded.Colors
	cfg.Plugins = loaded.Plugins
	cfg.Reflow = loaded.Reflow
	cfg.Settings = loaded.Settings
	// Auto-append plugin IDs only when the config file doesn't specify
	// segments at all (nil). If the user explicitly set segments — even to
	// an empty array — respect their choice and don't force plugins on.
	if loaded.Segments == nil {
		inSegments := make(map[string]bool, len(cfg.Segments))
		for _, id := range cfg.Segments {
			inSegments[id] = true
		}
		for _, p := range cfg.Plugins {
			if len(p.Fields) > 0 {
				for _, f := range p.Fields {
					if f.ID != "" && !inSegments[f.ID] {
						cfg.Segments = append(cfg.Segments, f.ID)
						inSegments[f.ID] = true
					}
				}
			} else if p.ID != "" && !inSegments[p.ID] {
				cfg.Segments = append(cfg.Segments, p.ID)
			}
		}
	}
	return cfg
}

func saveConfig(cfg config) error {
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}
