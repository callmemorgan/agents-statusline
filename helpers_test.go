package main

import (
	"testing"
	"time"
)

func TestVisibleWidth(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abc", 3},
		{"\x1b[35mabc\x1b[0m", 3},
		{"ctx ███░░ 65%", 13},
		{"\x1b[90m↑1.2M ↓89.0k\x1b[0m", 12},
	}
	for _, tc := range cases {
		if got := visibleWidth(tc.in); got != tc.want {
			t.Errorf("visibleWidth(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestFormatTokens(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1.0k"},
		{45678, "45.6k"},
		{999999, "999.9k"},
		{1000000, "1.0M"},
		{1234567, "1.2M"},
	}
	for _, tc := range cases {
		if got := formatTokens(tc.in); got != tc.want {
			t.Errorf("formatTokens(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFormatPath(t *testing.T) {
	cases := []struct {
		current, project, want string
	}{
		{"/Users/me/code/proj", "/Users/me/code/proj", "proj"},
		{"/Users/me/code/proj/sub", "/Users/me/code/proj", "proj→sub"},
		{"/Users/me/code/proj/a/b", "/Users/me/code/proj", "proj→a/b"},
		{"/Users/me/other", "/Users/me/code/proj", "other"},
		{"~", "", "~"},
	}
	for _, tc := range cases {
		if got := formatPath(tc.current, tc.project); got != tc.want {
			t.Errorf("formatPath(%q, %q) = %q, want %q", tc.current, tc.project, got, tc.want)
		}
	}
}

func TestFormatHHMMSS(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "00:00:00"},
		{-5, "00:00:00"},
		{3661000, "01:01:01"},
		{86399000, "23:59:59"},
	}
	for _, tc := range cases {
		if got := formatHHMMSS(tc.in); got != tc.want {
			t.Errorf("formatHHMMSS(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResetCountdown(t *testing.T) {
	now := time.Unix(1750000000, 0)
	cases := []struct {
		at   int64
		want string
	}{
		{now.Unix() - 10, "now"},
		{now.Unix() + 90, "1m"},
		{now.Unix() + 2*3600 + 30*60 + 30, "2h30m"},
		{now.Unix() + 3*86400 + 4*3600 + 60, "3d4h"},
	}
	for _, tc := range cases {
		if got := resetCountdown(tc.at, now); got != tc.want {
			t.Errorf("resetCountdown(now%+d) = %q, want %q", tc.at-now.Unix(), got, tc.want)
		}
	}
}

func TestEffortBadge(t *testing.T) {
	cases := map[string]string{
		"low": "⬇", "medium": "→", "high": "⬆", "xhigh": "⬆⬆", "max": "⬆⬆⬆",
		"HIGH": "⬆", "": "", "unknown": "",
	}
	for in, want := range cases {
		if got := effortBadge(in); got != want {
			t.Errorf("effortBadge(%q) = %q, want %q", in, got, want)
		}
	}
}

func ctxWindowSegment(t *testing.T) segmentInfo {
	t.Helper()
	initSegments(nil)
	seg, ok := segmentByID("context-window")
	if !ok {
		t.Fatal("context-window segment not registered")
	}
	return seg
}

func TestSettingsForDefaults(t *testing.T) {
	seg := ctxWindowSegment(t)
	s := settingsFor(config{}, seg)
	if !s.Bool("show_bar") || !s.Bool("show_warning") {
		t.Error("expected toggles to default to true")
	}
	if s.Int("bar_width") != 20 || s.Str("iconset") != "default" || s.Int("warn_at") != 60 || s.Int("crit_at") != 80 {
		t.Errorf("unexpected defaults: width=%d iconset=%q warn=%d crit=%d",
			s.Int("bar_width"), s.Str("iconset"), s.Int("warn_at"), s.Int("crit_at"))
	}
	if _, ok := s["show_countdown"]; ok {
		t.Error("context-window should not resolve a show_countdown setting")
	}
	if _, ok := s["stress_test"]; ok {
		t.Error("ephemeral specs must not appear in resolved settings")
	}
}

func TestSettingsForOverridesAndCoercion(t *testing.T) {
	seg := ctxWindowSegment(t)
	cfg := config{Settings: map[string]map[string]any{"context-window": {
		"bar_width": float64(35), // JSON numbers decode as float64
		"iconset":   "nonsense",  // invalid enum value → default
		"warn_at":   999,         // out of range → clamped
		"show_bar":  "yes",       // wrong type → default
	}}}
	s := settingsFor(cfg, seg)
	if s.Int("bar_width") != 35 {
		t.Errorf("float64 not coerced: %d", s.Int("bar_width"))
	}
	if s.Str("iconset") != "default" {
		t.Errorf("invalid enum should fall back to default: %q", s.Str("iconset"))
	}
	if s.Int("warn_at") != 100 {
		t.Errorf("out-of-range int should clamp: %d", s.Int("warn_at"))
	}
	if !s.Bool("show_bar") {
		t.Error("wrong-typed bool should fall back to default true")
	}
}

func TestPruneSettings(t *testing.T) {
	seg := ctxWindowSegment(t)
	s := settingsFor(config{}, seg)
	if got := pruneSettings(seg, s); got != nil {
		t.Errorf("all-default settings should prune to nil, got %v", got)
	}
	s["bar_width"] = 35
	got := pruneSettings(seg, s)
	if len(got) != 1 || got["bar_width"] != 35 {
		t.Errorf("expected only the changed key, got %v", got)
	}
}
