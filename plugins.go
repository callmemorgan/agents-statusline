package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ─── Plugin System ───────────────────────────────────────────────────

var registeredSegments []segmentInfo

// pluginCache holds per-command parsed output within a single buildStatusline
// call so multi-field plugins run their command only once per turn.
var pluginCache map[string]map[string]string

func clearPluginCache() {
	pluginCache = map[string]map[string]string{}
}

func initSegments(plugins []pluginDef) {
	registeredSegments = allSegmentInfos()
	for _, p := range plugins {
		def := p
		if len(def.Fields) > 0 {
			// Multi-field plugin: register one segment per field.
			for _, f := range def.Fields {
				field := f
				line := field.Line
				if line < 1 {
					line = 1
				}
				desc := field.Desc
				if desc == "" {
					desc = field.ID
				}
				registeredSegments = append(registeredSegments, segmentInfo{
					id:           field.ID,
					line:         line,
					desc:         desc + " [plugin]",
					primaryColor: "Dim",
					render: func(pay payload, c palette) (string, bool) {
						out := runPluginField(def, pay, field.ID)
						return out, out != ""
					},
				})
			}
		} else {
			// Single-field plugin: whole stdout is the segment value.
			line := def.Line
			if line < 1 {
				line = 1
			}
			desc := def.Desc
			if desc == "" {
				desc = def.ID
			}
			registeredSegments = append(registeredSegments, segmentInfo{
				id:           def.ID,
				line:         line,
				desc:         desc + " [plugin]",
				primaryColor: "Dim",
				render: func(pay payload, c palette) (string, bool) {
					out := runPluginRaw(def, pay)
					return out, out != ""
				},
			})
		}
	}
}

// runPluginField runs a multi-field plugin (cached per command) and returns
// the value for the requested field ID.
func runPluginField(def pluginDef, p payload, fieldID string) string {
	if pluginCache == nil {
		pluginCache = map[string]map[string]string{}
	}
	if _, ok := pluginCache[def.Command]; !ok {
		raw := runPluginRaw(def, p)
		pluginCache[def.Command] = parseKeyValueOutput(raw)
	}
	return pluginCache[def.Command][fieldID]
}

// parseKeyValueOutput parses "key:value" lines from plugin stdout.
func parseKeyValueOutput(raw string) map[string]string {
	result := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if idx := strings.IndexByte(line, ':'); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			if key != "" {
				result[key] = val
			}
		}
	}
	return result
}

// runPluginRaw executes the plugin command and returns the full trimmed stdout.
func runPluginRaw(def pluginDef, p payload) string {
	timeout := time.Duration(def.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 200 * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	home, _ := os.UserHomeDir()
	cmd := strings.Replace(def.Command, "~", home, 1)
	c := exec.CommandContext(ctx, cmd)

	session := p.SessionName
	if session == "" {
		session = p.ConversationID
	}
	c.Env = append(os.Environ(),
		"STATUSLINE_MODEL="+p.Model.DisplayName,
		"STATUSLINE_DIR="+p.Workspace.CurrentDir,
		"STATUSLINE_BRANCH="+p.Worktree.Branch,
		"STATUSLINE_SESSION="+session,
		"STATUSLINE_PRODUCT="+p.Product,
		"STATUSLINE_COLUMNS="+strconv.Itoa(p.TerminalWidth),
		"STATUSLINE_LINES="+os.Getenv("LINES"),
	)
	if raw, err := json.Marshal(p); err == nil {
		c.Env = append(c.Env, "STATUSLINE_PAYLOAD="+string(raw))
	}

	out, err := c.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
