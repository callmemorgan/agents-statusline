package main

// ─── Subagent StatusLine Hook ────────────────────────────────────────
//
// Claude Code's subagentStatusLine hook receives JSON with base payload fields
// plus a "columns" integer and a "tasks" array. This command emits one JSON
// line per task: {"id":"<task id>","content":"<ANSI string>"}. Empty ids keep
// the default rendering (we skip them), and empty content hides the row.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/callmemorgan/agents-statusline/internal/ansi"
	"github.com/callmemorgan/agents-statusline/internal/palette"
)

const maxSubagentInput = 1 << 20 // 1 MiB

// SubagentPayload is the JSON shape received on the subagentStatusLine hook.
type SubagentPayload struct {
	Columns int            `json:"columns"`
	Tasks   []SubagentTask `json:"tasks"`
}

// SubagentTask describes one subagent or tool task.
type SubagentTask struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	Status       string  `json:"status"`
	Description  string  `json:"description"`
	Label        string  `json:"label"`
	StartTime    int64   `json:"startTime"`
	TokenCount   int64   `json:"tokenCount"`
	TokenSamples []int64 `json:"tokenSamples"`
	Cwd          string  `json:"cwd"`
}

type subagentOutput struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

// RunSubagentStatusline reads a subagent payload from stdin and prints one
// JSON line per task. It uses the default theme so colors respect NO_COLOR and
// TERM=dumb automatically.
func RunSubagentStatusline() error {
	data, err := io.ReadAll(io.LimitReader(os.Stdin, maxSubagentInput))
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}
	data = bytes.TrimSpace(data)

	var p SubagentPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("parsing subagent payload: %w", err)
	}

	colors := palette.CurrentPalette("", "", nil)
	sep := colors.Dim + " · " + colors.Rst

	enc := json.NewEncoder(os.Stdout)
	for _, task := range p.Tasks {
		if task.ID == "" {
			continue
		}

		name := task.Name
		if strings.TrimSpace(name) == "" {
			continue
		}

		status := task.Status
		if status == "" {
			status = "unknown"
		}

		var right string
		if task.TokenCount > 0 {
			right = ansi.FormatTokens(task.TokenCount)
		} else if task.Description != "" {
			right = task.Description
		}

		statusColor := statusColorFor(task.Status, colors)
		content := name + sep + statusColor + status + colors.Rst
		if right != "" {
			content += sep + colors.Dim + right + colors.Rst
		}

		// Strip to catch the "all fields empty" case; otherwise ANSI-only
		// strings are still content.
		if strings.TrimSpace(ansi.StripANSI(content)) == "" {
			continue
		}

		if err := enc.Encode(subagentOutput{ID: task.ID, Content: content}); err != nil {
			return fmt.Errorf("encoding output: %w", err)
		}
	}

	return nil
}

// statusColorFor chooses the palette color used for a task status.
func statusColorFor(status string, colors palette.Palette) string {
	switch strings.ToLower(status) {
	case "done", "succeeded", "success", "completed":
		return colors.ROK
	case "running", "pending", "in_progress", "active":
		return colors.RWarn
	default:
		return colors.Dim
	}
}
