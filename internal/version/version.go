package version

// ─── Version ─────────────────────────────────────────────────────────

import (
	"fmt"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
)

// Set via GoReleaser ldflags (-X github.com/callmemorgan/agents-statusline/internal/version.Version=... etc).
// Source builds keep "dev" and fall back to module build info below.
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

// versionString returns the version, resolving from debug.ReadBuildInfo for
// `go install` / source builds that don't get ldflags.
func VersionString() (v, c, d string) {
	v, c, d = Version, Commit, Date
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

func RunVersion() {
	v, c, d := VersionString()
	fmt.Printf("agents-statusline v%s\n", v)
	if c != "" {
		fmt.Printf("  commit: %s\n", c)
	}
	if d != "" {
		fmt.Printf("  built:  %s\n", d)
	}
	fmt.Printf("  go:     %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

// reReleaseVersion matches a clean release-shaped version: MAJOR.MINOR.REVISION
// with no suffix. This rejects Go pseudo-versions ("0.1.0-0.20260612-abc"),
// "+dirty" / "+unknown" dev markers, and any other non-release build.
var reReleaseVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// IsReleaseVersion reports whether v is a clean MAJOR.MINOR.REVISION — the
// only form the takeover feature and update worker should fire on. Anything
// else (dev, dirty pseudo-versions, go-install @commit) is treated as a source
// build.
func IsReleaseVersion(v string) bool {
	return reReleaseVersion.MatchString(v)
}

// CompareVersions returns -1/0/+1 for MAJOR.MINOR.REVISION strings (leading
// "v" tolerated). Malformed input compares as equal-to-everything (0) so
// garbage from the network can never trigger an upgrade.
func CompareVersions(a, b string) int {
	pa, oka := ParseVersion(a)
	pb, okb := ParseVersion(b)
	if !oka || !okb {
		return 0
	}
	for i := 0; i < 3; i++ {
		if pa[i] < pb[i] {
			return -1
		}
		if pa[i] > pb[i] {
			return 1
		}
	}
	return 0
}

// ParseVersion parses a MAJOR.MINOR.REVISION string (leading "v" tolerated).
// It returns the three numeric components and true on success; malformed input
// returns false.
func ParseVersion(v string) ([3]int, bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if v == "" {
		return [3]int{}, false
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var out [3]int
	for i, p := range parts {
		// strconv.ParseUint rejects empty strings, signs, and non-digits, and
		// — unlike a hand-rolled n=n*10+digit loop — overflow. So an oversized
		// tag from the network parses as malformed (CompareVersions → 0) rather
		// than silently wrapping to a value that could look "newer".
		n, err := strconv.ParseUint(p, 10, 32)
		if err != nil {
			return [3]int{}, false
		}
		out[i] = int(n)
	}
	return out, true
}
