package main

import (
	"strconv"
	"time"

	"github.com/rivo/tview"
)

// ─── Flyout Test Segment ─────────────────────────────────────────────

// progressBarSegmentIDs returns the set of segments that share bar settings
// via "Sync to all bars". Derived from flyoutFeatures: any segment whose
// flyout contains a "bar_width" feature is considered a bar segment and
// participates in sync. This way adding a new bar segment only requires
// updating flyoutFeatures — the sync group follows automatically.
func progressBarSegmentIDs() []string {
	ids := make([]string, 0, len(flyoutFeatures))
	for id, features := range flyoutFeatures {
		for _, f := range features {
			if f.id == "bar_width" {
				ids = append(ids, id)
				break
			}
		}
	}
	return ids
}

type featureKind string

const (
	kindToggle featureKind = "toggle"
	kindCycle  featureKind = "cycle"
	kindNumber featureKind = "number"
)

type subFeature struct {
	id      string
	name    string
	desc    string
	kind    featureKind
	options []string // for cycle: ordered list of valid values
	min     int      // for number
	max     int      // for number
	step    int      // for number: per-press increment (1 for fine, 5+ for coarse)
}

// flyoutFeatures defines which segments have configurable sub-features.
var flyoutFeatures = map[string][]subFeature{
	"context-window": {
		{id: "show_bar", name: "Show bar", desc: "Render the progress bar", kind: kindToggle},
		{id: "show_warning", name: "Show >200k warning", desc: "Append red >200k when context exceeds 200k tokens", kind: kindToggle},
		{id: "bar_width", name: "Bar width", desc: "Number of characters in the progress bar", kind: kindNumber, min: 5, max: 50, step: 1},
		{id: "iconset", name: "Iconset", desc: "Visual style of the progress bar", kind: kindCycle, options: []string{"default", "blocks", "dots", "ascii", "minimal"}},
		{id: "warn_at", name: "Warn at", desc: "Percentage threshold for yellow warning color", kind: kindNumber, min: 0, max: 100, step: 5},
		{id: "crit_at", name: "Critical at", desc: "Percentage threshold for red critical color", kind: kindNumber, min: 0, max: 100, step: 5},
		{id: "ok_color", name: "OK color", desc: "Color below warning threshold", kind: kindCycle, options: colorCycle},
		{id: "warn_color", name: "Warn color", desc: "Color between warn and critical thresholds", kind: kindCycle, options: colorCycle},
		{id: "crit_color", name: "Critical color", desc: "Color above critical threshold", kind: kindCycle, options: colorCycle},
		{id: "stress_test", name: "Stress test preview", desc: "Animate preview from 0% to 100% to see all colors", kind: kindToggle},
	},
	"rate-limit-5h": {
		{id: "show_bar", name: "Show bar", desc: "Render the progress bar", kind: kindToggle},
		{id: "show_countdown", name: "Show countdown", desc: "Append (2h30m) countdown timer", kind: kindToggle},
		{id: "bar_width", name: "Bar width", desc: "Number of characters in the progress bar", kind: kindNumber, min: 5, max: 50, step: 1},
		{id: "iconset", name: "Iconset", desc: "Visual style of the progress bar", kind: kindCycle, options: []string{"default", "blocks", "dots", "ascii", "minimal"}},
		{id: "warn_at", name: "Warn at", desc: "Percentage threshold for yellow warning color", kind: kindNumber, min: 0, max: 100, step: 5},
		{id: "crit_at", name: "Critical at", desc: "Percentage threshold for red critical color", kind: kindNumber, min: 0, max: 100, step: 5},
		{id: "ok_color", name: "OK color", desc: "Color below warning threshold", kind: kindCycle, options: colorCycle},
		{id: "warn_color", name: "Warn color", desc: "Color between warn and critical thresholds", kind: kindCycle, options: colorCycle},
		{id: "crit_color", name: "Critical color", desc: "Color above critical threshold", kind: kindCycle, options: colorCycle},
		{id: "stress_test", name: "Stress test preview", desc: "Animate preview from 0% to 100% to see all colors", kind: kindToggle},
		{id: "sync_to_all", name: "Sync to all bars", desc: "Copy these settings to context-window and rate-limit-7d", kind: kindToggle},
	},
	"rate-limit-7d": {
		{id: "show_bar", name: "Show bar", desc: "Render the progress bar", kind: kindToggle},
		{id: "show_countdown", name: "Show countdown", desc: "Append (3d4h) countdown timer", kind: kindToggle},
		{id: "bar_width", name: "Bar width", desc: "Number of characters in the progress bar", kind: kindNumber, min: 5, max: 50, step: 1},
		{id: "iconset", name: "Iconset", desc: "Visual style of the progress bar", kind: kindCycle, options: []string{"default", "blocks", "dots", "ascii", "minimal"}},
		{id: "warn_at", name: "Warn at", desc: "Percentage threshold for yellow warning color", kind: kindNumber, min: 0, max: 100, step: 5},
		{id: "crit_at", name: "Critical at", desc: "Percentage threshold for red critical color", kind: kindNumber, min: 0, max: 100, step: 5},
		{id: "ok_color", name: "OK color", desc: "Color below warning threshold", kind: kindCycle, options: colorCycle},
		{id: "warn_color", name: "Warn color", desc: "Color between warn and critical thresholds", kind: kindCycle, options: colorCycle},
		{id: "crit_color", name: "Critical color", desc: "Color above critical threshold", kind: kindCycle, options: colorCycle},
		{id: "stress_test", name: "Stress test preview", desc: "Animate preview from 0% to 100% to see all colors", kind: kindToggle},
		{id: "sync_to_all", name: "Sync to all bars", desc: "Copy these settings to context-window and rate-limit-5h", kind: kindToggle},
	},
}

// ─── Flyout Helpers ──────────────────────────────────────────────────

func ptrBool(v bool) *bool    { return &v }
func ptrInt(v int) *int       { return &v }
func ptrStr(v string) *string { return &v }

// stressTestActive tracks which flyout segments have stress-test preview enabled.
var stressTestActive = map[string]bool{}
var stressTestTimers = map[string]*time.Timer{}

func scheduleStressTick(app *tview.Application, segID string, updateFn func()) {
	stressTestTimers[segID] = time.AfterFunc(50*time.Millisecond, func() {
		app.QueueUpdateDraw(func() {
			if stressTestActive[segID] {
				updateFn()
				scheduleStressTick(app, segID, updateFn)
			}
		})
	})
}

func stopStressTest(segID string) {
	delete(stressTestActive, segID)
	if t, ok := stressTestTimers[segID]; ok {
		t.Stop()
		delete(stressTestTimers, segID)
	}
}

func flyoutValueStr(segID string, f subFeature, cfg config) string {
	s := settingsFor(cfg, segID)
	switch f.kind {
	case kindToggle:
		switch f.id {
		case "show_bar":
			if *s.ShowBar {
				return "on"
			}
		case "show_countdown":
			if *s.ShowCountdown {
				return "on"
			}
		case "show_warning":
			if *s.ShowWarning {
				return "on"
			}
		case "stress_test":
			if stressTestActive[segID] {
				return "on"
			}
		}
		return "off"
	case kindCycle:
		switch f.id {
		case "iconset":
			return *s.Iconset
		case "ok_color":
			return *s.OkColor
		case "warn_color":
			return *s.WarnColor
		case "crit_color":
			return *s.CritColor
		}
	case kindNumber:
		switch f.id {
		case "bar_width":
			return strconv.Itoa(*s.BarWidth)
		case "warn_at":
			return strconv.Itoa(*s.WarnAt)
		case "crit_at":
			return strconv.Itoa(*s.CritAt)
		}
	}
	return ""
}

func cycleOption(options []string, current string, delta int) string {
	idx := 0
	for i, o := range options {
		if o == current {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(options)) % len(options)
	return options[idx]
}

func applyFlyoutToggle(segID string, f subFeature, cfg *config) {
	s := settingsFor(*cfg, segID)
	switch f.id {
	case "show_bar":
		s.ShowBar = ptrBool(!*s.ShowBar)
	case "show_countdown":
		s.ShowCountdown = ptrBool(!*s.ShowCountdown)
	case "show_warning":
		s.ShowWarning = ptrBool(!*s.ShowWarning)
	case "stress_test":
		stressTestActive[segID] = !stressTestActive[segID]
		return // don't save stress_test to cfg.Settings
	}
	if cfg.Settings == nil {
		cfg.Settings = map[string]segmentSettings{}
	}
	cfg.Settings[segID] = pruneSettings(segID, s)
}

func applyFlyoutCycle(segID string, f subFeature, cfg *config, delta int) {
	s := settingsFor(*cfg, segID)
	cur := ""
	switch f.id {
	case "iconset":
		cur = *s.Iconset
	case "ok_color":
		cur = *s.OkColor
	case "warn_color":
		cur = *s.WarnColor
	case "crit_color":
		cur = *s.CritColor
	}
	next := cycleOption(f.options, cur, delta)
	switch f.id {
	case "iconset":
		s.Iconset = &next
	case "ok_color":
		s.OkColor = &next
	case "warn_color":
		s.WarnColor = &next
	case "crit_color":
		s.CritColor = &next
	}
	if cfg.Settings == nil {
		cfg.Settings = map[string]segmentSettings{}
	}
	cfg.Settings[segID] = pruneSettings(segID, s)
}

func applyFlyoutNumber(segID string, f subFeature, cfg *config, delta int) {
	s := settingsFor(*cfg, segID)
	var v int
	switch f.id {
	case "bar_width":
		v = *s.BarWidth
	case "warn_at":
		v = *s.WarnAt
	case "crit_at":
		v = *s.CritAt
	}
	v += delta
	if v < f.min {
		v = f.min
	}
	if v > f.max {
		v = f.max
	}
	switch f.id {
	case "bar_width":
		s.BarWidth = &v
	case "warn_at":
		s.WarnAt = &v
	case "crit_at":
		s.CritAt = &v
	}
	if cfg.Settings == nil {
		cfg.Settings = map[string]segmentSettings{}
	}
	cfg.Settings[segID] = pruneSettings(segID, s)
}

// flyoutPreviewPayload returns a payload modified for the flyout preview.
// If stress test is active, it overrides the percentage fields so the preview
// animates through all threshold states.
func flyoutPreviewPayload(segID string, base payload) payload {
	if !stressTestActive[segID] {
		return base
	}
	p := base
	pct := int((time.Now().UnixMilli() % 2000) * 100 / 2000)
	switch segID {
	case "context-window":
		p.Exceeds200K = ptrBool(pct > 80)
		p.ContextWindow.UsedPercentage = ptrFloat64(float64(pct))
	case "rate-limit-5h":
		p.RateLimits.FiveHour.UsedPercentage = ptrFloat64(float64(pct))
	case "rate-limit-7d":
		p.RateLimits.SevenDay.UsedPercentage = ptrFloat64(float64(pct))
	}
	return p
}

func ptrFloat64(v float64) *float64 { return &v }

func cloneSettings(s segmentSettings) segmentSettings {
	c := segmentSettings{}
	if s.ShowBar != nil {
		v := *s.ShowBar
		c.ShowBar = &v
	}
	if s.ShowCountdown != nil {
		v := *s.ShowCountdown
		c.ShowCountdown = &v
	}
	if s.ShowWarning != nil {
		v := *s.ShowWarning
		c.ShowWarning = &v
	}
	if s.BarWidth != nil {
		v := *s.BarWidth
		c.BarWidth = &v
	}
	if s.Iconset != nil {
		v := *s.Iconset
		c.Iconset = &v
	}
	if s.WarnAt != nil {
		v := *s.WarnAt
		c.WarnAt = &v
	}
	if s.CritAt != nil {
		v := *s.CritAt
		c.CritAt = &v
	}
	if s.OkColor != nil {
		v := *s.OkColor
		c.OkColor = &v
	}
	if s.WarnColor != nil {
		v := *s.WarnColor
		c.WarnColor = &v
	}
	if s.CritColor != nil {
		v := *s.CritColor
		c.CritColor = &v
	}
	return c
}

// pruneSettings drops fields from s that the renderer for segID never reads,
// so the saved config doesn't accumulate dead keys. The settingsFor default
// (which is the source of these fields) populates every field for every
// segment; without pruning, opening the flyout on a segment and changing one
// setting would write the whole defaulted struct back.
func pruneSettings(segID string, s segmentSettings) segmentSettings {
	switch segID {
	case "context-window":
		// context-window ignores ShowCountdown (only rate-limit-* use it).
		s.ShowCountdown = nil
	case "rate-limit-5h", "rate-limit-7d":
		// rate-limit-* ignore ShowWarning (only context-window uses it).
		s.ShowWarning = nil
	}
	return s
}
