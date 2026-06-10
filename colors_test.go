package main

import (
	"strings"
	"testing"
)

func clearColorEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"NO_COLOR", "TERM", "COLORTERM", "TERM_PROGRAM", "WT_SESSION"} {
		t.Setenv(k, "")
	}
}

func TestDetectDepth(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want colorDepth
	}{
		{"no_color", map[string]string{"NO_COLOR": "1", "COLORTERM": "truecolor"}, depthNone},
		{"dumb", map[string]string{"TERM": "dumb"}, depthNone},
		{"colorterm", map[string]string{"COLORTERM": "truecolor", "TERM": "xterm"}, depthTrue},
		{"iterm", map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM": "xterm"}, depthTrue},
		{"256", map[string]string{"TERM": "xterm-256color"}, depth256},
		{"plain", map[string]string{"TERM": "xterm"}, depth16},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearColorEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if got := detectDepth(); got != tc.want {
				t.Errorf("detectDepth() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResolveDepthOverride(t *testing.T) {
	clearColorEnv(t)
	t.Setenv("TERM", "xterm")
	if got := resolveDepth("truecolor"); got != depthTrue {
		t.Errorf("override truecolor = %v", got)
	}
	if got := resolveDepth("none"); got != depthNone {
		t.Errorf("override none = %v", got)
	}
	if got := resolveDepth(""); got != depth16 {
		t.Errorf("auto on plain xterm = %v", got)
	}
	t.Setenv("NO_COLOR", "1")
	if got := resolveDepth("truecolor"); got != depthNone {
		t.Errorf("NO_COLOR must beat config override, got %v", got)
	}
}

func TestParseHexRGB(t *testing.T) {
	if r, g, b, ok := parseHexRGB("#cba6f7"); !ok || r != 0xcb || g != 0xa6 || b != 0xf7 {
		t.Errorf("parseHexRGB = %d,%d,%d,%v", r, g, b, ok)
	}
	for _, bad := range []string{"", "#fff", "#zzzzzz", "12345", "#1234567"} {
		if _, _, _, ok := parseHexRGB(bad); ok {
			t.Errorf("parseHexRGB(%q) should fail", bad)
		}
	}
}

func TestRgbTo256(t *testing.T) {
	cases := []struct {
		r, g, b, want int
	}{
		{0, 0, 0, 16},        // cube black
		{255, 255, 255, 231}, // cube white
		{255, 0, 0, 196},     // pure red
		{0, 215, 0, 40},      // cube green level 3
		{128, 128, 128, 244}, // mid gray → gray ramp
	}
	for _, tc := range cases {
		if got := rgbTo256(tc.r, tc.g, tc.b); got != tc.want {
			t.Errorf("rgbTo256(%d,%d,%d) = %d, want %d", tc.r, tc.g, tc.b, got, tc.want)
		}
	}
}

func TestHexEscapeDepths(t *testing.T) {
	if esc, ok := hexEscape("#ff0000", depthTrue); !ok || esc != "\x1b[38;2;255;0;0m" {
		t.Errorf("truecolor escape = %q %v", esc, ok)
	}
	if esc, ok := hexEscape("#ff0000", depth256); !ok || esc != "\x1b[38;5;196m" {
		t.Errorf("256 escape = %q %v", esc, ok)
	}
	if esc, ok := hexEscape("#ff0000", depth16); !ok || esc != colorCodes["bright-red"] {
		t.Errorf("16-color quantization = %q %v", esc, ok)
	}
	if esc, ok := hexEscape("#ff0000", depthNone); !ok || esc != "" {
		t.Errorf("none depth should be empty, got %q", esc)
	}
}

// TestClassicThemeMatchesLegacyPalette is the back-compat gate: the default
// theme must reproduce the pre-1.0 hardcoded escape codes exactly.
func TestClassicThemeMatchesLegacyPalette(t *testing.T) {
	for _, d := range []colorDepth{depth16, depth256, depthTrue} {
		p := resolvePalette(themeByID("classic"), d)
		want := map[string]string{
			"Model": "\x1b[35m", "Dir": "\x1b[36m", "Git": "\x1b[32m",
			"Chg": "\x1b[33m", "Dur": "\x1b[34m", "Cost": "\x1b[33m",
			"Dim": "\x1b[90m", "Rst": "\x1b[0m", "ROK": "\x1b[32m",
			"RWarn": "\x1b[33m", "RCrit": "\x1b[91m", "Agent": "\x1b[95m",
			"Vim": "\x1b[97m", "Purple": "\x1b[35m", "Session": "\x1b[96m",
			// Classic separators are uncolored, matching the pre-1.0 joiner.
			"Sep": "",
		}
		got := map[string]string{
			"Model": p.Model, "Dir": p.Dir, "Git": p.Git, "Chg": p.Chg,
			"Dur": p.Dur, "Cost": p.Cost, "Dim": p.Dim, "Rst": p.Rst,
			"ROK": p.ROK, "RWarn": p.RWarn, "RCrit": p.RCrit,
			"Agent": p.Agent, "Vim": p.Vim, "Purple": p.Purple,
			"Session": p.Session, "Sep": p.Sep,
		}
		for k, w := range want {
			if got[k] != w {
				t.Errorf("classic@depth%d %s = %q, want %q", d, k, got[k], w)
			}
		}
	}
}

func TestThemedPaletteDepths(t *testing.T) {
	nord := themeByID("nord")
	pTrue := resolvePalette(nord, depthTrue)
	if pTrue.Git != "\x1b[38;2;163;190;140m" { // #a3be8c
		t.Errorf("nord truecolor git = %q", pTrue.Git)
	}
	p256 := resolvePalette(nord, depth256)
	if !strings.HasPrefix(p256.Git, "\x1b[38;5;") {
		t.Errorf("nord 256 git = %q", p256.Git)
	}
	p16 := resolvePalette(nord, depth16)
	if !strings.HasPrefix(p16.Git, "\x1b[") || strings.Contains(p16.Git, ";") {
		t.Errorf("nord 16-color git should be a basic escape, got %q", p16.Git)
	}
	if p := resolvePalette(nord, depthNone); p.Rst != "" {
		t.Error("depthNone must yield an empty palette")
	}
}

func TestResolveColorSpec(t *testing.T) {
	p := resolvePalette(themeByID("nord"), depthTrue)
	cases := []struct {
		spec string
		want string
		ok   bool
	}{
		{"", "", false},
		{"default", "", false},
		{"#ff0000", "\x1b[38;2;255;0;0m", true},
		{"196", "\x1b[38;5;196m", true},
		{"magenta", "\x1b[35m", true},
		{"accent", "\x1b[38;2;94;129;172m", true}, // nord #5e81ac
		{"bogus", "", false},
		{"999", "", false},
	}
	for _, tc := range cases {
		got, ok := resolveColorSpec(tc.spec, p)
		if got != tc.want || ok != tc.ok {
			t.Errorf("resolveColorSpec(%q) = %q,%v want %q,%v", tc.spec, got, ok, tc.want, tc.ok)
		}
	}
	// Disabled palette resolves nothing.
	if _, ok := resolveColorSpec("#ff0000", palette{}); ok {
		t.Error("disabled palette must not resolve specs")
	}
}

func TestApplyThemeOverrides(t *testing.T) {
	tm := applyThemeOverrides(themeByID("nord"), map[string]string{
		"git":   "#112233",
		"cost":  "yellow",
		"dim":   "245",
		"bogus": "#ffffff", // unknown role ignored
	})
	if tm.Roles["git"].Hex != "#112233" {
		t.Errorf("hex override not applied: %+v", tm.Roles["git"])
	}
	if tm.Roles["cost"].Ansi16 != colorCodes["yellow"] || tm.Roles["cost"].Hex != "" {
		t.Errorf("name override not applied: %+v", tm.Roles["cost"])
	}
	if tm.Roles["dim"].Hex == "" {
		t.Errorf("256-index override should set hex: %+v", tm.Roles["dim"])
	}
	if _, ok := tm.Roles["bogus"]; ok {
		t.Error("unknown role must not be added")
	}
	// Original untouched.
	if themeByID("nord").Roles["git"].Hex != "#a3be8c" {
		t.Error("applyThemeOverrides mutated the builtin theme")
	}
}

func TestCurrentPaletteConfig(t *testing.T) {
	clearColorEnv(t)
	t.Setenv("TERM", "xterm")
	p := currentPalette(config{Theme: "dracula", ColorDepth: "truecolor"})
	if p.RCrit != "\x1b[38;2;255;85;85m" { // #ff5555
		t.Errorf("dracula crit = %q", p.RCrit)
	}
	if p := currentPalette(config{ColorDepth: "none"}); p.Rst != "" {
		t.Error("color_depth none should disable colors")
	}
}

func TestValidateConfigThemeKeys(t *testing.T) {
	cfg := config{Theme: "vaporwave", ColorDepth: "8bit", ThemeColors: map[string]string{"git": "notacolor", "ghost": "#fff"}}
	warns := validateConfig(&cfg)
	if cfg.Theme != "" || cfg.ColorDepth != "" {
		t.Errorf("bad theme/depth should reset: %+v", cfg)
	}
	if len(cfg.ThemeColors) != 0 {
		t.Errorf("bad theme_colors entries should drop: %v", cfg.ThemeColors)
	}
	if len(warns) < 4 {
		t.Errorf("expected 4+ warnings, got %v", warns)
	}
}
