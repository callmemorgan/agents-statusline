package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const subagentSampleInput = `{
  "columns": 120,
  "tasks": [
    {"id":"task-1","name":"Research","type":"tool","status":"running","description":"Searching web","tokenCount":1500},
    {"id":"task-2","name":"Review","type":"subagent","status":"done","description":"Code review","tokenCount":0},
    {"id":"","name":"Skipped","type":"tool","status":"pending","description":"no id","tokenCount":0},
    {"id":"task-3","name":"","type":"","status":"","description":"","tokenCount":0}
  ]
}`

func TestRunSubagentStatusline(t *testing.T) {
	oldIn, oldOut := os.Stdin, os.Stdout
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = inR
	go func() {
		_, _ = inW.WriteString(subagentSampleInput)
		_ = inW.Close()
	}()

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = outW

	if err := RunSubagentStatusline(); err != nil {
		t.Fatalf("RunSubagentStatusline: %v", err)
	}
	_ = outW.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(outR); err != nil {
		t.Fatal(err)
	}
	_ = outR.Close()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 output lines, got %d:\n%s", len(lines), buf.String())
	}

	var first subagentOutput
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("first line not JSON: %v\n%s", err, lines[0])
	}
	if first.ID != "task-1" {
		t.Errorf("first id = %q, want task-1", first.ID)
	}
	if !strings.Contains(first.Content, "Research") {
		t.Errorf("first content missing name: %s", first.Content)
	}
	if !strings.Contains(first.Content, "running") {
		t.Errorf("first content missing status: %s", first.Content)
	}
	if !strings.Contains(first.Content, "1.5k") && !strings.Contains(first.Content, "1500") {
		t.Errorf("first content missing token count: %s", first.Content)
	}

	var second subagentOutput
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("second line not JSON: %v\n%s", err, lines[1])
	}
	if second.ID != "task-2" {
		t.Errorf("second id = %q, want task-2", second.ID)
	}
	if !strings.Contains(second.Content, "Review") {
		t.Errorf("second content missing name: %s", second.Content)
	}
	if !strings.Contains(second.Content, "done") {
		t.Errorf("second content missing status: %s", second.Content)
	}
	if !strings.Contains(second.Content, "Code review") {
		t.Errorf("second content missing description fallback: %s", second.Content)
	}
}

func TestRunSubagentStatuslineEmptyInput(t *testing.T) {
	oldIn, oldOut := os.Stdin, os.Stdout
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = inR
	go func() { _ = inW.Close() }()

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = outW

	err = RunSubagentStatusline()
	_ = outW.Close()

	if err == nil {
		t.Fatal("expected error for empty input")
	}

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(outR)
	_ = outR.Close()
	if strings.TrimSpace(buf.String()) != "" {
		t.Errorf("expected no output on error, got %q", buf.String())
	}
}
