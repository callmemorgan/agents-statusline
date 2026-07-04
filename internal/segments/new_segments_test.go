package segments

import (
	"strings"
	"testing"

	"github.com/callmemorgan/claude-statusline/internal/config"
	"github.com/callmemorgan/claude-statusline/internal/payload"
)

func renderSeg(t *testing.T, p payload.Payload, id string, cfg config.Config) (string, bool) {
	t.Helper()
	Init()
	seg, ok := ByID(id)
	if !ok {
		t.Fatalf("segment %q not registered", id)
	}
	s := config.SettingsFor(cfg, seg.ID, seg.Settings)
	return seg.Render(RenderCtx{P: p, S: s, Now: testNow})
}

func TestPromptIDSegment(t *testing.T) {
	var p payload.Payload
	if _, show := renderSeg(t, p, "prompt-id", config.Config{}); show {
		t.Error("prompt-id should hide when empty")
	}
	p.PromptID = "abc123"
	if got, show := renderSeg(t, p, "prompt-id", config.Config{}); !show || got != "prompt:abc123" {
		t.Errorf("prompt-id short = %q, %v", got, show)
	}
	p.PromptID = "550e8400-e29b-41d4-a716-446655440000"
	if got, show := renderSeg(t, p, "prompt-id", config.Config{}); !show || got != "prompt:550e8400" {
		t.Errorf("prompt-id uuid = %q, %v", got, show)
	}
	p.PromptID = "550e8400e29b41d4a716446655440000" // 32 chars, no dashes
	if got, show := renderSeg(t, p, "prompt-id", config.Config{}); !show || got != "prompt:"+p.PromptID {
		t.Errorf("prompt-id non-uuid = %q, %v", got, show)
	}
}

func TestPRSegment(t *testing.T) {
	var p payload.Payload
	if _, show := renderSeg(t, p, "pr", config.Config{}); show {
		t.Error("pr should hide when PR.Number is 0")
	}
	p.PR.Number = 42
	p.PR.ReviewState = "approved"
	if got, show := renderSeg(t, p, "pr", config.Config{}); !show || got != "#42 (approved)" {
		t.Errorf("pr default = %q, %v", got, show)
	}
	cfg := config.Config{Settings: map[string]map[string]any{"pr": {"show_review_state": false}}}
	if got, show := renderSeg(t, p, "pr", cfg); !show || got != "#42" {
		t.Errorf("pr no state = %q, %v", got, show)
	}
	cfg = config.Config{Settings: map[string]map[string]any{"pr": {"show_url": true}}}
	p.PR.URL = "https://github.com/callmemorgan/claude-statusline/pull/42"
	if got, show := renderSeg(t, p, "pr", cfg); !show || !strings.HasPrefix(got, "https://") {
		t.Errorf("pr full url = %q, %v", got, show)
	}
	p.PR.URL = ""
	p.Workspace.Repo = payload.Repo{Host: "github.com", Owner: "callmemorgan", Name: "claude-statusline"}
	if got, show := renderSeg(t, p, "pr", cfg); !show || got != "https://github.com/callmemorgan/claude-statusline/pull/42" {
		t.Errorf("pr synthesized url = %q, %v", got, show)
	}
	p.Workspace.Repo = payload.Repo{}
	if got, show := renderSeg(t, p, "pr", cfg); !show || got != "#42" {
		t.Errorf("pr url fallback = %q, %v", got, show)
	}
}

func TestRepoSegment(t *testing.T) {
	var p payload.Payload
	if _, show := renderSeg(t, p, "repo", config.Config{}); show {
		t.Error("repo should hide when Repo.Name is empty")
	}
	p.Workspace.Repo = payload.Repo{Owner: "callmemorgan", Name: "claude-statusline"}
	if got, show := renderSeg(t, p, "repo", config.Config{}); !show || got != "callmemorgan/claude-statusline" {
		t.Errorf("repo default = %q, %v", got, show)
	}
	cfg := config.Config{Settings: map[string]map[string]any{"repo": {"show_host": true}}}
	p.Workspace.Repo.Host = "github.com"
	if got, show := renderSeg(t, p, "repo", cfg); !show || got != "github.com:callmemorgan/claude-statusline" {
		t.Errorf("repo with host = %q, %v", got, show)
	}
}

func TestGitBranchWorktreeEnrichment(t *testing.T) {
	p := payload.Payload{
		Worktree: payload.Worktree{Branch: "feature/config"},
	}
	if got, show := renderSeg(t, p, "git-branch", config.Config{}); !show || got != "feature/config" {
		t.Errorf("git-branch plain = %q, %v", got, show)
	}
	p.Worktree.Path = "/Users/me/code/my-project"
	cfg := config.Config{Settings: map[string]map[string]any{"git-branch": {"show_worktree_path": true}}}
	if got, show := renderSeg(t, p, "git-branch", cfg); !show || got != "feature/config (/Users/me/code/my-project)" {
		t.Errorf("git-branch path = %q, %v", got, show)
	}
	p.Worktree.OriginalBranch = "main"
	cfg = config.Config{Settings: map[string]map[string]any{"git-branch": {"show_original_branch": true}}}
	if got, show := renderSeg(t, p, "git-branch", cfg); !show || got != "feature/config ←main" {
		t.Errorf("git-branch original = %q, %v", got, show)
	}
}

func TestThinkingSegment(t *testing.T) {
	var p payload.Payload
	if _, show := renderSeg(t, p, "thinking", config.Config{}); show {
		t.Error("thinking should hide when Enabled is nil")
	}
	falseVal := false
	p.Thinking.Enabled = &falseVal
	if _, show := renderSeg(t, p, "thinking", config.Config{}); show {
		t.Error("thinking should hide when Enabled is false")
	}
	trueVal := true
	p.Thinking.Enabled = &trueVal
	if got, show := renderSeg(t, p, "thinking", config.Config{}); !show || got != "🗘 thinking" {
		t.Errorf("thinking emoji = %q, %v", got, show)
	}
	cfg := config.Config{Settings: map[string]map[string]any{"thinking": {"icon": "text"}}}
	if got, show := renderSeg(t, p, "thinking", cfg); !show || got != "[thinking]" {
		t.Errorf("thinking text = %q, %v", got, show)
	}
}
