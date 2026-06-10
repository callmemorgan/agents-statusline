package main

// ─── Version ─────────────────────────────────────────────────────────

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

// Set via GoReleaser ldflags (-X main.version=... etc). Source builds keep
// "dev" and fall back to module build info below.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

// versionString returns the version, resolving from debug.ReadBuildInfo for
// `go install` / source builds that don't get ldflags.
func versionString() (v, c, d string) {
	v, c, d = version, commit, date
	if v != "dev" {
		return v, c, d
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return v, c, d
	}
	if mv := info.Main.Version; mv != "" && mv != "(devel)" {
		v = strings.TrimPrefix(mv, "v")
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if c == "" && len(s.Value) >= 7 {
				c = s.Value[:7]
			}
		case "vcs.time":
			if d == "" {
				d = s.Value
			}
		}
	}
	return v, c, d
}

func runVersion() {
	v, c, d := versionString()
	fmt.Printf("claude-statusline v%s\n", v)
	if c != "" {
		fmt.Printf("  commit: %s\n", c)
	}
	if d != "" {
		fmt.Printf("  built:  %s\n", d)
	}
	fmt.Printf("  go:     %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
