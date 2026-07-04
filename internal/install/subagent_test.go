package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallSubagentStatusLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(settingsSample), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := installSubagentStatusLine(path, false, true, false, true); err != nil {
		t.Fatalf("installSubagentStatusLine: %v", err)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("result does not parse: %v\n%s", err, out)
	}
	raw, ok := parsed["subagentStatusLine"]
	if !ok {
		t.Fatalf("subagentStatusLine missing from %s", out)
	}
	if !strings.Contains(string(raw), `"type": "command"`) {
		t.Errorf("subagentStatusLine missing type: %s", raw)
	}
	if !strings.Contains(string(raw), `"command":`) {
		t.Errorf("subagentStatusLine missing command: %s", raw)
	}
	if !strings.Contains(string(raw), "subagent-statusline") {
		t.Errorf("subagentStatusLine command does not reference subagent-statusline: %s", raw)
	}

	// Original keys and formatting should be preserved.
	if !strings.Contains(string(out), `"model": "opus"`) {
		t.Error("original model key was disturbed")
	}
	if !strings.Contains(string(out), "1.50") {
		t.Error("number formatting was mangled")
	}
}

func TestInstallSubagentStatusLineDryRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(settingsSample), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := installSubagentStatusLine(path, false, true, true, true); err != nil {
		t.Fatalf("installSubagentStatusLine dry-run: %v", err)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "subagentStatusLine") {
		t.Error("dry-run wrote to disk")
	}
}
