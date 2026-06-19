package version

import (
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "1.99.99", 1},
		{"v1.2.0", "1.1.9", 1},
		{"1.2.0", "v1.2.0", 0},
		{"1.10.0", "1.9.99", 1},
		{"malformed", "1.0.0", 0},
		{"1.0.0", "garbage", 0},
		{"", "1.0.0", 0},
		{"v", "1.0.0", 0},
		{"1.0", "1.0.0", 0},
		{"1.0.0.0", "1.0.0", 0},
		{"1.-1.0", "1.0.0", 0},
		{"1.0.0+meta", "1.0.0", 0}, // strict: +meta is treated as malformed
	}
	for _, tc := range cases {
		got := CompareVersions(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
	// Property: malformed never compares greater.
	bad := []string{"", "v", "1", "1.0", "1.0.0.0", "1.x.0", "1.-1.0", "1.0.0+meta", "  ", "abc"}
	for _, b := range bad {
		if CompareVersions(b, "1.0.0") > 0 {
			t.Errorf("malformed %q compared greater than 1.0.0", b)
		}
		if CompareVersions("1.0.0", b) < 0 {
			t.Errorf("1.0.0 compared less than malformed %q", b)
		}
	}
}

func TestIsReleaseVersion(t *testing.T) {
	cases := []struct {
		v    string
		want bool
	}{
		{"1.1.0", true},
		{"0.0.0", true},
		{"10.20.30", true},
		{"dev", false},
		{"", false},
		{"1.0.2+dirty", false},
		{"1.0.2+unknown", false},
		// Go pseudo-versions from `go install @commit`:
		{"0.1.0-0.20260612120000-abc123abc123", false},
		{"v0.0.0-20240101120000-abc123abc123", false},
		// 4-part not allowed:
		{"1.2.3.4", false},
		// 2-part not allowed:
		{"1.2", false},
		// Leading v not allowed (we strip it earlier in the pipeline):
		{"v1.1.0", false},
	}
	for _, tc := range cases {
		t.Run(tc.v, func(t *testing.T) {
			if got := IsReleaseVersion(tc.v); got != tc.want {
				t.Errorf("IsReleaseVersion(%q) = %v, want %v", tc.v, got, tc.want)
			}
		})
	}
}
