package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePath_Explicit(t *testing.T) {
	p, err := ResolvePath("/tmp/custom.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != "/tmp/custom.md" {
		t.Fatalf("got %q, want /tmp/custom.md", p)
	}
}

func TestResolvePath_Default(t *testing.T) {
	p, err := ResolvePath("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cwd, _ := os.Getwd()
	want := filepath.Join(cwd, "WORKFLOW.md")
	if p != want {
		t.Fatalf("got %q, want %q", p, want)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/WORKFLOW.md")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "missing_workflow_file") {
		t.Fatalf("expected missing_workflow_file error, got: %v", err)
	}
}

func TestLoad_FromTestdata(t *testing.T) {
	tests := []struct {
		file       string
		wantPrompt string
		wantConfig map[string]any
	}{
		{
			file:       "testdata/minimal.md",
			wantPrompt: "You are working on an issue.",
			wantConfig: map[string]any{},
		},
		{
			file:       "testdata/prompt_only.md",
			wantPrompt: "Just a prompt, no front matter.",
			wantConfig: map[string]any{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			wf, err := Load(tt.file)
			if err != nil {
				t.Fatalf("Load(%q) error: %v", tt.file, err)
			}
			if wf.PromptTemplate != tt.wantPrompt {
				t.Errorf("prompt = %q, want %q", wf.PromptTemplate, tt.wantPrompt)
			}
			if len(wf.Config) != len(tt.wantConfig) {
				t.Errorf("config len = %d, want %d", len(wf.Config), len(tt.wantConfig))
			}
		})
	}
}

func TestParse_FullFrontMatter(t *testing.T) {
	content := `---
tracker:
  kind: linear
  project_slug: test-slug
polling:
  interval_ms: 5000
---
Hello {{ issue.identifier }}
`
	wf, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if wf.PromptTemplate != "Hello {{ issue.identifier }}" {
		t.Errorf("prompt = %q", wf.PromptTemplate)
	}
	tracker, ok := wf.Config["tracker"].(map[string]any)
	if !ok {
		t.Fatal("tracker config missing or wrong type")
	}
	if tracker["kind"] != "linear" {
		t.Errorf("tracker.kind = %v, want linear", tracker["kind"])
	}
	if tracker["project_slug"] != "test-slug" {
		t.Errorf("tracker.project_slug = %v", tracker["project_slug"])
	}
	polling, ok := wf.Config["polling"].(map[string]any)
	if !ok {
		t.Fatal("polling config missing")
	}
	if polling["interval_ms"] != 5000 {
		t.Errorf("polling.interval_ms = %v, want 5000", polling["interval_ms"])
	}
}

func TestParse_EmptyFrontMatter(t *testing.T) {
	content := "---\n---\nJust prompt."
	wf, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(wf.Config) != 0 {
		t.Errorf("expected empty config, got %v", wf.Config)
	}
	if wf.PromptTemplate != "Just prompt." {
		t.Errorf("prompt = %q", wf.PromptTemplate)
	}
}

func TestParse_NoFrontMatter(t *testing.T) {
	content := "No front matter here.\nSecond line."
	wf, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(wf.Config) != 0 {
		t.Errorf("expected empty config, got %v", wf.Config)
	}
	if wf.PromptTemplate != "No front matter here.\nSecond line." {
		t.Errorf("prompt = %q", wf.PromptTemplate)
	}
}

func TestParse_FrontMatterNotAMap(t *testing.T) {
	content := "---\n- item1\n- item2\n---\nPrompt."
	_, err := Parse(content)
	if err == nil {
		t.Fatal("expected error for non-map front matter")
	}
	if !strings.Contains(err.Error(), "workflow_front_matter_not_a_map") {
		t.Fatalf("expected front_matter_not_a_map error, got: %v", err)
	}
}

func TestParse_InvalidYAML(t *testing.T) {
	content := "---\n{{{invalid yaml\n---\nPrompt."
	_, err := Parse(content)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	if !strings.Contains(err.Error(), "workflow_parse_error") {
		t.Fatalf("expected workflow_parse_error, got: %v", err)
	}
}

func TestParse_PromptTrimmed(t *testing.T) {
	content := "---\ntracker:\n  kind: linear\n---\n\n  Hello  \n\n"
	wf, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if wf.PromptTemplate != "Hello" {
		t.Errorf("prompt = %q, want %q", wf.PromptTemplate, "Hello")
	}
}

func TestParse_NestedConfig(t *testing.T) {
	content := `---
hooks:
  after_create: |
    git clone --depth 1 https://example.com .
  timeout_ms: 30000
agent:
  max_concurrent_agents_by_state:
    todo: 3
    in progress: 5
---
Prompt body.
`
	wf, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	hooks, ok := wf.Config["hooks"].(map[string]any)
	if !ok {
		t.Fatal("hooks missing")
	}
	if hooks["timeout_ms"] != 30000 {
		t.Errorf("hooks.timeout_ms = %v", hooks["timeout_ms"])
	}
	agent, ok := wf.Config["agent"].(map[string]any)
	if !ok {
		t.Fatal("agent missing")
	}
	byState, ok := agent["max_concurrent_agents_by_state"].(map[string]any)
	if !ok {
		t.Fatal("max_concurrent_agents_by_state missing")
	}
	if byState["todo"] != 3 {
		t.Errorf("by_state[todo] = %v", byState["todo"])
	}
}
