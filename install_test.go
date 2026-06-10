package main

import (
	"encoding/json"
	"strings"
	"testing"
)

const settingsSample = `{
  "model": "opus",
  "env": {"FOO": "bar"},
  "permissions": {
    "allow": ["Bash(ls:*)"]
  },
  "weird_number": 1.50
}
`

func TestFindTopLevelKeySpan(t *testing.T) {
	raw := []byte(settingsSample)
	keyStart, valStart, valEnd, found, err := findTopLevelKeySpan(raw, "env")
	if err != nil || !found {
		t.Fatalf("env not found: %v", err)
	}
	if got := string(raw[keyStart:valEnd]); got != `"env": {"FOO": "bar"}` {
		t.Errorf("span = %q", got)
	}
	if got := string(raw[valStart:valEnd]); got != `{"FOO": "bar"}` {
		t.Errorf("value span = %q", got)
	}
	if _, _, _, found, _ := findTopLevelKeySpan(raw, "FOO"); found {
		t.Error("nested keys must not match at top level")
	}
	if _, _, _, found, _ := findTopLevelKeySpan(raw, "missing"); found {
		t.Error("missing key reported found")
	}
}

func TestInsertTopLevelKeyPreservesBytes(t *testing.T) {
	out, err := insertTopLevelKey([]byte(settingsSample), "statusLine", `{"type": "command", "command": "claude-statusline"}`)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("result does not parse: %v\n%s", err, out)
	}
	if _, ok := parsed["statusLine"]; !ok {
		t.Error("statusLine missing after insert")
	}
	// Every original byte sequence survives: the file minus our inserted
	// line equals the original.
	stripped := strings.Replace(string(out), "\n  \"statusLine\": {\"type\": \"command\", \"command\": \"claude-statusline\"},", "", 1)
	if stripped != settingsSample {
		t.Errorf("original bytes were disturbed:\n%s", out)
	}
	if !strings.Contains(string(out), "1.50") {
		t.Error("number formatting mangled")
	}
}

func TestInsertIntoEmptyObject(t *testing.T) {
	for _, in := range []string{"{}", "{}\n", "{\n}\n", "  {   }  "} {
		out, err := insertTopLevelKey([]byte(in), "statusline", `"claude-statusline"`)
		if err != nil {
			t.Fatalf("insert into %q: %v", in, err)
		}
		var parsed map[string]string
		if err := json.Unmarshal(out, &parsed); err != nil {
			t.Fatalf("result does not parse: %v\n%s", err, out)
		}
		if parsed["statusline"] != "claude-statusline" {
			t.Errorf("value wrong: %v", parsed)
		}
	}
}

func TestInsertSniffsIndent(t *testing.T) {
	fourSpace := "{\n    \"a\": 1\n}\n"
	out, _ := insertTopLevelKey([]byte(fourSpace), "k", `"v"`)
	if !strings.Contains(string(out), "\n    \"k\": \"v\",") {
		t.Errorf("4-space indent not sniffed:\n%s", out)
	}
	tabbed := "{\n\t\"a\": 1\n}\n"
	out, _ = insertTopLevelKey([]byte(tabbed), "k", `"v"`)
	if !strings.Contains(string(out), "\n\t\"k\": \"v\",") {
		t.Errorf("tab indent not sniffed:\n%s", out)
	}
}

func TestReplaceKeyValue(t *testing.T) {
	raw := []byte(settingsSample)
	_, valStart, valEnd, _, _ := findTopLevelKeySpan(raw, "model")
	out := replaceKeyValue(raw, valStart, valEnd, `"sonnet"`)
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("does not parse: %v", err)
	}
	if string(parsed["model"]) != `"sonnet"` {
		t.Errorf("model = %s", parsed["model"])
	}
	if !strings.Contains(string(out), `"weird_number": 1.50`) {
		t.Error("other keys disturbed")
	}
}

func TestDeleteTopLevelKey(t *testing.T) {
	cases := []struct {
		name, key string
	}{
		{"first", "model"},
		{"middle", "permissions"},
		{"last", "weird_number"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, found, err := deleteTopLevelKey([]byte(settingsSample), tc.key)
			if err != nil || !found {
				t.Fatalf("delete failed: %v found=%v", err, found)
			}
			var parsed map[string]json.RawMessage
			if err := json.Unmarshal(out, &parsed); err != nil {
				t.Fatalf("does not parse after deleting %s: %v\n%s", tc.key, err, out)
			}
			if _, ok := parsed[tc.key]; ok {
				t.Errorf("%s still present", tc.key)
			}
			if len(parsed) != 3 {
				t.Errorf("expected 3 keys left, got %d: %s", len(parsed), out)
			}
		})
	}

	// Only key.
	out, found, err := deleteTopLevelKey([]byte("{\n  \"only\": 1\n}\n"), "only")
	if err != nil || !found {
		t.Fatal("only-key delete failed")
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(out, &parsed); err != nil || len(parsed) != 0 {
		t.Errorf("only-key delete result: %s (err %v)", out, err)
	}

	// Missing key.
	if _, found, _ := deleteTopLevelKey([]byte(settingsSample), "nope"); found {
		t.Error("missing key reported found")
	}
}

func TestDeleteMiddleKeyExactFormatting(t *testing.T) {
	in := "{\n  \"a\": 1,\n  \"statusLine\": {\"type\": \"command\"},\n  \"b\": 2\n}\n"
	out, _, err := deleteTopLevelKey([]byte(in), "statusLine")
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"a\": 1,\n  \"b\": 2\n}\n"
	if string(out) != want {
		t.Errorf("formatting after delete:\ngot  %q\nwant %q", out, want)
	}
}

func TestParseGateRejectsJSONC(t *testing.T) {
	jsonc := []byte("{\n  // comment\n  \"a\": 1\n}\n")
	var gate map[string]json.RawMessage
	if err := json.Unmarshal(jsonc, &gate); err == nil {
		t.Fatal("expected JSONC to fail the parse gate")
	}
}

func TestResolveTargetAgyProbe(t *testing.T) {
	dir := t.TempDir()
	homeDirOverride = dir
	t.Cleanup(func() { homeDirOverride = "" })

	if _, err := resolveTarget("agy", ""); err == nil {
		t.Error("agy with no settings file should require --settings-path")
	}
	if _, err := resolveTarget("bogus", ""); err == nil {
		t.Error("unknown target should error")
	}
	tgt, err := resolveTarget("claude", "")
	if err != nil || !strings.HasSuffix(tgt.path, "/.claude/settings.json") {
		t.Errorf("claude target: %+v err=%v", tgt, err)
	}
	if !strings.Contains(tgt.value, `"type": "command"`) {
		t.Errorf("claude value: %s", tgt.value)
	}
}
