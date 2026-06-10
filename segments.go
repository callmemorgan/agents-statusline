package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ─── Segment Renderers ───────────────────────────────────────────────

func renderVimMode(p payload, c palette) (string, bool) {
	if p.Vim.Mode == "" {
		return "", false
	}
	return c.Vim + "[" + p.Vim.Mode + "]" + c.Rst, true
}

func renderSessionName(p payload, c palette) (string, bool) {
	name := firstNonEmpty(p.SessionName, p.ConversationID)
	if name == "" {
		return "", false
	}
	if len(name) == 36 && strings.Count(name, "-") == 4 {
		name = name[:8]
	}
	return c.Session + name + c.Rst, true
}

func renderAgentName(p payload, c palette) (string, bool) {
	if p.Agent.Name == "" {
		return "", false
	}
	return c.Agent + p.Agent.Name + c.Rst, true
}

func renderDirectory(p payload, c palette) (string, bool) {
	currentDir := firstNonEmpty(p.Workspace.CurrentDir, p.Cwd, "~")
	projectDir := p.Workspace.ProjectDir
	return c.Dir + formatPath(currentDir, projectDir) + c.Rst, true
}

func renderGitBranch(p payload, c palette) (string, bool) {
	currentDir := firstNonEmpty(p.Workspace.CurrentDir, p.Cwd, "~")
	branch := p.Worktree.Branch
	if branch == "" {
		branch = gitBranch(currentDir)
	}
	if branch == "" {
		return "", false
	}
	worktreeName := p.Worktree.Name
	if worktreeName == "" {
		worktreeName = p.Workspace.GitWorktree
	}
	display := branch
	if worktreeName != "" && worktreeName != branch {
		display = branch + " " + c.Dim + "(" + worktreeName + ")" + c.Rst
	}
	return c.Git + display + c.Rst, true
}

func renderLinesChanged(p payload, c palette) (string, bool) {
	if p.Cost.TotalLinesAdded == 0 && p.Cost.TotalLinesRemoved == 0 {
		return "", false
	}
	return c.Chg + fmt.Sprintf("+%d/-%d", p.Cost.TotalLinesAdded, p.Cost.TotalLinesRemoved) + c.Rst, true
}

func renderCachePercent(p payload, c palette) (string, bool) {
	cacheTotal := p.ContextWindow.CurrentUsage.InputTokens +
		p.ContextWindow.CurrentUsage.CacheCreationInputTokens +
		p.ContextWindow.CurrentUsage.CacheReadInputTokens
	if cacheTotal <= 0 || p.ContextWindow.CurrentUsage.CacheReadInputTokens <= 0 {
		return "", false
	}
	cacheBP := p.ContextWindow.CurrentUsage.CacheReadInputTokens * 10000 / cacheTotal
	return c.Dim + fmt.Sprintf("cache:%d.%02d%%", cacheBP/100, cacheBP%100) + c.Rst, true
}

func renderCost(p payload, c palette) (string, bool) {
	if p.Cost.TotalCostUSD == 0 {
		return "", false
	}
	return c.Cost + "$" + formatCost(p.Cost.TotalCostUSD) + c.Rst, true
}

func renderModel(p payload, c palette) (string, bool) {
	modelName := firstNonEmpty(p.Model.DisplayName, p.Model.ID, "Claude")
	effortLevel := p.Effort.Level
	if effortLevel == "" {
		effortLevel = readEffortLevel()
	}
	modelLabel := modelName
	badge := effortBadge(effortLevel)
	if badge != "" {
		modelLabel += " " + badge
	}
	return c.Model + "[" + modelLabel + "]" + c.Rst, true
}

func renderVersion(p payload, c palette) (string, bool) {
	if p.Version == "" {
		return "", false
	}
	return c.Dim + "v" + p.Version + c.Rst, true
}

func renderDuration(p payload, c palette) (string, bool) {
	if p.Cost.TotalDurationMS == 0 {
		return "", false
	}
	elapsed := formatHHMMSS(p.Cost.TotalDurationMS)
	return c.Dur + elapsed + c.Rst, true
}

func renderAPIEfficiency(p payload, c palette) (string, bool) {
	if p.Cost.TotalDurationMS <= 0 {
		return "", false
	}
	return fmt.Sprintf("%s(API:%d%%)%s", c.Dim, p.Cost.TotalAPIDuration*100/p.Cost.TotalDurationMS, c.Rst), true
}

func renderTokens(p payload, c palette) (string, bool) {
	inStr := formatTokens(p.ContextWindow.TotalInputTokens)
	outStr := formatTokens(p.ContextWindow.TotalOutputTokens)
	return c.Dim + "↑" + inStr + " ↓" + outStr + c.Rst, true
}

func renderContextWindow(p payload, c palette) (string, bool) {
	ctxPct := 0
	if p.ContextWindow.UsedPercentage != nil {
		ctxPct = int(*p.ContextWindow.UsedPercentage)
	} else {
		usageTokens := p.ContextWindow.CurrentUsage.InputTokens +
			p.ContextWindow.CurrentUsage.CacheCreationInputTokens +
			p.ContextWindow.CurrentUsage.CacheReadInputTokens
		if usageTokens == 0 {
			usageTokens = p.ContextWindow.TotalInputTokens
		}
		if p.ContextWindow.ContextWindowSize > 0 && usageTokens > 0 {
			ctxPct = int(usageTokens * 100 / p.ContextWindow.ContextWindowSize)
		}
	}
	s := settingsFor(buildCfg, "context-window")
	ctxColor := pctColorWithSettings(ctxPct, c, s)
	result := c.Dim + "ctx "
	if *s.ShowBar {
		result += progressBarWithIconset(ctxPct, ctxColor, c.Dim, c, *s.BarWidth, *s.Iconset) + " "
	}
	result += ctxColor + strconv.Itoa(ctxPct) + "%" + c.Rst
	if *s.ShowWarning && p.Exceeds200K != nil && *p.Exceeds200K {
		result += " " + c.RCrit + ">200k" + c.Rst
	}
	return result, true
}

func renderRateLimit5h(p payload, c palette) (string, bool) {
	return rateLimitSegment("5h", p.RateLimits.FiveHour, 5*3600, c, settingsFor(buildCfg, "rate-limit-5h"))
}

func renderRateLimit7d(p payload, c palette) (string, bool) {
	return rateLimitSegment("7d", p.RateLimits.SevenDay, 7*24*3600, c, settingsFor(buildCfg, "rate-limit-7d"))
}

func renderAgentState(p payload, c palette) (string, bool) {
	if p.AgentState == "" {
		return "", false
	}
	stateColor := c.Dim
	if p.AgentState == "working" {
		stateColor = c.Git
	}
	return stateColor + "[" + p.AgentState + "]" + c.Rst, true
}

func renderSandbox(p payload, c palette) (string, bool) {
	if !p.Sandbox.Enabled {
		return "", false
	}
	return c.RCrit + "[SANDBOX]" + c.Rst, true
}

func renderArtifactCount(p payload, c palette) (string, bool) {
	if p.ArtifactCount <= 0 {
		return "", false
	}
	return c.Chg + fmt.Sprintf("artifacts:%d", p.ArtifactCount) + c.Rst, true
}

func renderPlanTier(p payload, c palette) (string, bool) {
	if p.PlanTier == "" {
		return "", false
	}
	return c.Purple + p.PlanTier + c.Rst, true
}

// ─── Segment Registry ────────────────────────────────────────────────

type segmentInfo struct {
	id           string
	line         int
	desc         string
	primaryColor string
	render       func(p payload, c palette) (string, bool)
}

func allSegmentInfos() []segmentInfo {
	return []segmentInfo{
		{id: "vim-mode", line: 1, desc: "Vim mode indicator (e.g. [normal])", primaryColor: "Vim", render: renderVimMode},
		{id: "sandbox", line: 1, desc: "Sandbox status indicator", primaryColor: "RCrit", render: renderSandbox},
		{id: "session-name", line: 1, desc: "Session name label", primaryColor: "Session", render: renderSessionName},
		{id: "agent-state", line: 1, desc: "Agent working status", primaryColor: "Git", render: renderAgentState},
		{id: "agent-name", line: 1, desc: "Agent name", primaryColor: "Agent", render: renderAgentName},
		{id: "directory", line: 1, desc: "Current / project directory", primaryColor: "Dir", render: renderDirectory},
		{id: "git-branch", line: 1, desc: "Git branch and worktree name", primaryColor: "Git", render: renderGitBranch},
		{id: "artifact-count", line: 1, desc: "Artifact count", primaryColor: "Chg", render: renderArtifactCount},
		{id: "lines-changed", line: 1, desc: "All lines added / removed by the agent in the session", primaryColor: "Chg", render: renderLinesChanged},
		{id: "cache-percent", line: 1, desc: "Cache read percentage", primaryColor: "Dim", render: renderCachePercent},
		{id: "plan-tier", line: 1, desc: "Subscription plan tier", primaryColor: "Purple", render: renderPlanTier},
		{id: "cost", line: 1, desc: "Total session cost", primaryColor: "Cost", render: renderCost},
		{id: "model", line: 2, desc: "Model name and effort badge", primaryColor: "Model", render: renderModel},
		{id: "version", line: 2, desc: "Claude Code version", primaryColor: "Dim", render: renderVersion},
		{id: "duration", line: 2, desc: "Elapsed session duration", primaryColor: "Dur", render: renderDuration},
		{id: "api-efficiency", line: 2, desc: "API efficiency percentage", primaryColor: "Dim", render: renderAPIEfficiency},
		{id: "tokens", line: 2, desc: "Input / output token counts", primaryColor: "Dim", render: renderTokens},
		{id: "context-window", line: 3, desc: "Context window usage bar", primaryColor: "Dim", render: renderContextWindow},
		{id: "rate-limit-5h", line: 3, desc: "5-hour quota bar", primaryColor: "Dim", render: renderRateLimit5h},
		{id: "rate-limit-7d", line: 3, desc: "7-day quota bar", primaryColor: "Dim", render: renderRateLimit7d},
	}
}

func segmentByID(id string) (segmentInfo, bool) {
	for _, s := range registeredSegments {
		if s.id == id {
			return s, true
		}
	}
	return segmentInfo{}, false
}

// ─── Helpers ─────────────────────────────────────────────────────────

func formatPath(current, project string) string {
	display := filepath.Base(current)
	if display == "." || display == string(filepath.Separator) || display == "" {
		display = current
	}
	if project != "" && current != project && strings.HasPrefix(current, project+"/") {
		return filepath.Base(project) + "→" + strings.TrimPrefix(current, project+"/")
	}
	return display
}

func gitBranch(dir string) string {
	searchDir, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		gitEntry := filepath.Join(searchDir, ".git")
		info, err := os.Stat(gitEntry)
		if err == nil {
			gitDir := gitEntry
			if !info.IsDir() {
				data, readErr := os.ReadFile(gitEntry)
				if readErr != nil {
					return ""
				}
				ref := strings.TrimSpace(string(data))
				if !strings.HasPrefix(ref, "gitdir: ") {
					return ""
				}
				gitDir = strings.TrimPrefix(ref, "gitdir: ")
				if !filepath.IsAbs(gitDir) {
					gitDir = filepath.Clean(filepath.Join(searchDir, gitDir))
				}
			}
			head, readErr := os.ReadFile(filepath.Join(gitDir, "HEAD"))
			if readErr != nil {
				return ""
			}
			content := strings.TrimSpace(string(head))
			if strings.HasPrefix(content, "ref: refs/heads/") {
				return strings.TrimPrefix(content, "ref: refs/heads/")
			}
			return "detached"
		}
		parent := filepath.Dir(searchDir)
		if parent == searchDir {
			return ""
		}
		searchDir = parent
	}
}

func formatHHMMSS(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	totalSeconds := ms / 1000
	return fmt.Sprintf("%02d:%02d:%02d", totalSeconds/3600, (totalSeconds%3600)/60, totalSeconds%60)
}

func formatTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%d.%dM", n/1_000_000, (n%1_000_000)/100_000)
	case n >= 1_000:
		return fmt.Sprintf("%d.%dk", n/1_000, (n%1_000)/100)
	default:
		return strconv.FormatInt(n, 10)
	}
}

func rateLimitSegment(label string, window limitWindow, windowSecs int64, c palette, s segmentSettings) (string, bool) {
	if window.UsedPercentage == nil {
		return "", false
	}
	pct := int(*window.UsedPercentage)
	color := pctColorWithSettings(pct, c, s)
	countdown := "?"
	timePct := -1
	if window.ResetsAt != nil {
		countdown = resetCountdown(*window.ResetsAt)
		if windowSecs > 0 {
			remaining := *window.ResetsAt - time.Now().Unix()
			if remaining >= 0 && remaining <= windowSecs {
				timePct = int((windowSecs - remaining) * 100 / windowSecs)
			}
		}
	}
	result := c.Dim + label + " "
	if *s.ShowBar {
		result += progressBarWithTimeAndIconset(pct, timePct, color, c.Dim, c, *s.BarWidth, *s.Iconset) + " "
	}
	result += color + strconv.Itoa(pct) + "%" + c.Dim
	if *s.ShowCountdown {
		result += " (" + countdown + ")"
	}
	result += c.Rst
	return result, true
}

func resetCountdown(resetUnix int64) string {
	remaining := resetUnix - time.Now().Unix()
	if remaining <= 0 {
		return "now"
	}
	days := remaining / 86400
	hours := (remaining % 86400) / 3600
	minutes := (remaining % 3600) / 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd%dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh%02dm", hours, minutes)
	default:
		return fmt.Sprintf("%dm", minutes)
	}
}

func formatCost(v float64) string {
	return fmt.Sprintf("%.2f", v)
}

func effortBadge(effort string) string {
	switch strings.ToLower(effort) {
	case "low":
		return "⬇"
	case "medium":
		return "→"
	case "high":
		return "⬆"
	case "xhigh":
		return "⬆⬆"
	case "max":
		return "⬆⬆⬆"
	default:
		return ""
	}
}

func readEffortLevel() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		return ""
	}
	var s struct {
		EffortLevel string `json:"effortLevel"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return ""
	}
	return s.EffortLevel
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
