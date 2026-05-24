package workflow

import (
	"strings"
	"testing"

	"github.com/kwanpham2195/symphony-go/internal"
)

func TestRenderPrompt_BasicInterpolation(t *testing.T) {
	tmpl := "Working on {{ issue.identifier }}: {{ issue.title }}"
	issue := internal.Issue{
		Identifier: "ABC-123",
		Title:      "Fix bug",
	}
	out, err := RenderPrompt(tmpl, issue, nil)
	if err != nil {
		t.Fatalf("RenderPrompt error: %v", err)
	}
	if out != "Working on ABC-123: Fix bug" {
		t.Errorf("got %q", out)
	}
}

func TestRenderPrompt_WithAttempt(t *testing.T) {
	tmpl := `{% if attempt %}Retry #{{ attempt }}{% endif %}Issue: {{ issue.identifier }}`
	issue := internal.Issue{Identifier: "X-1"}
	attempt := 3
	out, err := RenderPrompt(tmpl, issue, &attempt)
	if err != nil {
		t.Fatalf("RenderPrompt error: %v", err)
	}
	if !strings.Contains(out, "Retry #3") {
		t.Errorf("expected retry info, got %q", out)
	}
	if !strings.Contains(out, "Issue: X-1") {
		t.Errorf("expected issue, got %q", out)
	}
}

func TestRenderPrompt_NoAttempt(t *testing.T) {
	tmpl := `{% if attempt %}Retry{% endif %}First run for {{ issue.identifier }}`
	issue := internal.Issue{Identifier: "Y-2"}
	out, err := RenderPrompt(tmpl, issue, nil)
	if err != nil {
		t.Fatalf("RenderPrompt error: %v", err)
	}
	if strings.Contains(out, "Retry") {
		t.Errorf("should not contain Retry on first run, got %q", out)
	}
}

func TestRenderPrompt_EmptyTemplate(t *testing.T) {
	issue := internal.Issue{Identifier: "Z-3"}
	out, err := RenderPrompt("", issue, nil)
	if err != nil {
		t.Fatalf("RenderPrompt error: %v", err)
	}
	if out != DefaultPromptTemplate {
		t.Errorf("got %q, want default", out)
	}
}

func TestRenderPrompt_DescriptionConditional(t *testing.T) {
	tmpl := `{% if issue.description %}{{ issue.description }}{% else %}No description.{% endif %}`

	t.Run("with description", func(t *testing.T) {
		issue := internal.Issue{Description: "Fix the login flow"}
		out, err := RenderPrompt(tmpl, issue, nil)
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if out != "Fix the login flow" {
			t.Errorf("got %q", out)
		}
	})

	t.Run("without description", func(t *testing.T) {
		issue := internal.Issue{}
		out, err := RenderPrompt(tmpl, issue, nil)
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if out != "No description." {
			t.Errorf("got %q", out)
		}
	})
}

func TestRenderPrompt_Labels(t *testing.T) {
	tmpl := "Labels: {{ issue.labels }}"
	issue := internal.Issue{Labels: []string{"bug", "urgent"}}
	out, err := RenderPrompt(tmpl, issue, nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(out, "bug") || !strings.Contains(out, "urgent") {
		t.Errorf("got %q", out)
	}
}

func TestRenderPrompt_Priority(t *testing.T) {
	tmpl := "Priority: {{ issue.priority }}"
	p := 1
	issue := internal.Issue{Priority: &p}
	out, err := RenderPrompt(tmpl, issue, nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(out, "1") {
		t.Errorf("got %q", out)
	}
}

func TestRenderPrompt_UpstreamWorkflowShape(t *testing.T) {
	// Test with a prompt shape similar to the upstream WORKFLOW.md
	tmpl := `You are working on a Linear ticket {{ issue.identifier }}

{% if attempt %}
Continuation context:
- This is retry attempt #{{ attempt }}.
{% endif %}

Issue context:
Identifier: {{ issue.identifier }}
Title: {{ issue.title }}
Current status: {{ issue.state }}
Labels: {{ issue.labels }}
URL: {{ issue.url }}

Description:
{% if issue.description %}
{{ issue.description }}
{% else %}
No description provided.
{% endif %}`

	issue := internal.Issue{
		Identifier:  "SYM-42",
		Title:       "Implement workflow parsing",
		State:       "In Progress",
		Labels:      []string{"feature", "priority"},
		URL:         "https://linear.app/team/SYM-42",
		Description: "Parse WORKFLOW.md with front matter.",
	}

	out, err := RenderPrompt(tmpl, issue, nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(out, "SYM-42") {
		t.Errorf("missing identifier in output")
	}
	if !strings.Contains(out, "Implement workflow parsing") {
		t.Errorf("missing title in output")
	}
	if !strings.Contains(out, "Parse WORKFLOW.md") {
		t.Errorf("missing description in output")
	}
	if strings.Contains(out, "Continuation context") {
		t.Error("should not have continuation on first run")
	}

	// Now with attempt
	attempt := 2
	out2, err := RenderPrompt(tmpl, issue, &attempt)
	if err != nil {
		t.Fatalf("error on retry: %v", err)
	}
	if !strings.Contains(out2, "retry attempt #2") {
		t.Errorf("missing retry info, got:\n%s", out2)
	}
}
