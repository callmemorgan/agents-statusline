package main

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	barWidth  = 20
	maxInput  = 1 << 20
	minObject = `{"model":{"display_name":"Claude"},"workspace":{"current_dir":"~"}}`
)

var reANSI = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return reANSI.ReplaceAllString(s, "")
}

func visibleWidth(s string) int {
	return utf8.RuneCountInString(stripANSI(s))
}

// ─── Statusline Builder ──────────────────────────────────────────────

// buildCfg holds the config for the current build so that segment renderers
// can access per-segment settings without threading cfg through every signature.
var buildCfg config

func buildStatusline(p payload, c palette, cfg config, columns int) []string {
	buildCfg = cfg
	clearPluginCache()
	parts := map[int][]string{}
	for _, id := range cfg.Segments {
		if s, ok := segmentByID(id); ok {
			segPalette := c
			if c.Rst != "" {
				if colorName := cfg.Colors[id]; colorName != "" && colorName != "default" {
					segPalette = paletteWithOverride(c, s.primaryColor, colorName)
				}
			}
			if rendered, show := s.render(p, segPalette); show {
				line := s.line
				if override, ok := cfg.Lines[id]; ok && override >= 1 {
					line = override
				}
				parts[line] = append(parts[line], rendered)
			}
		}
	}
	if len(parts) == 0 {
		return []string{}
	}

	if columns > 0 && cfg.Reflow == "group" {
		return buildStatuslineGroup(parts, columns)
	}

	return buildStatuslineCascade(parts, columns)
}

// buildStatuslineCascade is the original reflow behaviour: segments spill
// greedily from line 1 → 2 → 3 regardless of which logical line they belong to.
func buildStatuslineCascade(parts map[int][]string, columns int) []string {
	maxLine := 0
	originalLines := map[int]bool{}
	for k := range parts {
		if k > maxLine {
			maxLine = k
		}
		originalLines[k] = true
	}

	// Track which lines received overflow from a previous line.
	receivedOverflow := map[int]bool{}

	// Auto-reflow: spill trailing segments to the next line when a line
	// exceeds the available terminal width.
	if columns > 0 {
		const timingSuffixReserve = 15
		const safetyMargin = 5
		lineNum := 1
		for lineNum <= maxLine {
			budget := columns - safetyMargin
			if lineNum == 1 {
				budget = columns - timingSuffixReserve - safetyMargin
				if budget < 10 {
					budget = 10
				}
			}
			for {
				segs := parts[lineNum]
				if len(segs) <= 1 {
					break
				}
				width := 1 // leading space in joinParts
				for i, seg := range segs {
					if i > 0 {
						width += 3 // " │ "
					}
					width += visibleWidth(seg)
				}
				if width <= budget {
					break
				}
				// Move last segment to the next line.
				moved := segs[len(segs)-1]
				parts[lineNum] = segs[:len(segs)-1]
				parts[lineNum+1] = append([]string{moved}, parts[lineNum+1]...)
				receivedOverflow[lineNum+1] = true
				if lineNum+1 > maxLine {
					maxLine = lineNum + 1
				}
			}
			lineNum++
		}
	}

	out := []string{}
	for i := 1; i <= maxLine; i++ {
		line := joinParts(parts[i])
		if receivedOverflow[i] && originalLines[i] && i > 1 && (len(out) == 0 || out[len(out)-1] != "") {
			out = append(out, "")
		}
		out = append(out, line)
	}
	return out
}

// buildStatuslineGroup wraps each logical line independently. Segments from
// different logical lines never share a physical line, preserving the line
// boundaries defined in the configuration.
func buildStatuslineGroup(parts map[int][]string, columns int) []string {
	const timingSuffixReserve = 15
	const safetyMargin = 5

	var lineNums []int
	for k := range parts {
		lineNums = append(lineNums, k)
	}
	sort.Ints(lineNums)

	var out []string
	firstPhysicalLine := true

	for _, lineNum := range lineNums {
		segs := parts[lineNum]
		if len(segs) == 0 {
			continue
		}

		var current []string
		currentWidth := 0

		for _, seg := range segs {
			segWidth := visibleWidth(seg)
			sep := 1 // leading space
			if len(current) > 0 {
				sep = 3 // " │ "
			}

			budget := columns - safetyMargin
			if firstPhysicalLine && len(out) == 0 {
				budget = columns - timingSuffixReserve - safetyMargin
				if budget < 10 {
					budget = 10
				}
			}

			if len(current) == 0 || currentWidth+sep+segWidth <= budget {
				current = append(current, seg)
				currentWidth += sep + segWidth
			} else {
				out = append(out, joinParts(current))
				current = []string{seg}
				currentWidth = 1 + segWidth
				firstPhysicalLine = false
			}
		}

		if len(current) > 0 {
			out = append(out, joinParts(current))
			firstPhysicalLine = false
		}
	}

	return out
}

func joinParts(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	return " " + strings.Join(parts, " │ ")
}

// iconsetChars maps iconset names to (filled, empty) runes.
var iconsetChars = map[string][2]string{
	"default": {"#", "-"},
	"blocks":  {"█", "░"},
	"dots":    {"●", "○"},
	"ascii":   {"=", " "},
	"minimal": {"|", " "},
}

func iconsetPair(name string) (string, string) {
	if p, ok := iconsetChars[name]; ok {
		return p[0], p[1]
	}
	return "#", "-"
}

func progressBarWithIconset(pct int, fillColor, emptyColor string, c palette, width int, iconset string) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filledChar, emptyChar := iconsetPair(iconset)
	filled := pct * width / 100
	empty := width - filled
	return fillColor + strings.Repeat(filledChar, filled) + emptyColor + strings.Repeat(emptyChar, empty) + c.Rst
}

func progressBarWithTimeAndIconset(pct, timePct int, fillColor, emptyColor string, c palette, width int, iconset string) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := pct * width / 100
	filledChar, emptyChar := iconsetPair(iconset)

	timeSlot := -1
	if timePct >= 0 && timePct <= 100 {
		timeSlot = timePct * width / 100
		if timeSlot >= width {
			timeSlot = width - 1
		}
	}

	var b strings.Builder
	for i := 0; i < width; i++ {
		switch {
		case i == timeSlot:
			b.WriteString(c.Purple + "|")
		case i < filled:
			b.WriteString(fillColor + filledChar)
		default:
			b.WriteString(emptyColor + emptyChar)
		}
	}
	b.WriteString(c.Rst)
	return b.String()
}

func pctColorWithSettings(pct int, c palette, s segmentSettings) string {
	warnAt := 60
	critAt := 80
	if s.WarnAt != nil {
		warnAt = *s.WarnAt
	}
	if s.CritAt != nil {
		critAt = *s.CritAt
	}
	var colorName string
	switch {
	case pct > critAt:
		colorName = pickColor(s.CritColor, "bright-red")
	case pct >= warnAt:
		colorName = pickColor(s.WarnColor, "yellow")
	default:
		colorName = pickColor(s.OkColor, "green")
	}
	return resolveColor(colorName, c)
}
