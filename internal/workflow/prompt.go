package workflow

import (
	"fmt"
	"strings"

	"github.com/kwanpham2195/symphony-go/internal"
	"github.com/osteele/liquid"
)

// DefaultPromptTemplate is used when the workflow body is empty.
const DefaultPromptTemplate = "You are working on an issue from Linear."

// RenderPrompt renders the workflow prompt template with the given issue and
// optional attempt number. It uses Liquid-compatible template syntax.
//
// Template variables:
//   - issue: normalized issue object (all fields)
//   - attempt: retry/continuation attempt number (nil on first run)
func RenderPrompt(tmpl string, issue internal.Issue, attempt *int) (string, error) {
	if strings.TrimSpace(tmpl) == "" {
		tmpl = DefaultPromptTemplate
	}

	bindings := buildBindings(issue, attempt)

	engine := liquid.NewEngine()
	out, err := engine.ParseAndRenderString(tmpl, bindings)
	if err != nil {
		return "", fmt.Errorf("template_render_error: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// buildBindings converts shared types into a map suitable for Liquid rendering.
// Liquid expects map[string]interface{} for nested access.
func buildBindings(issue internal.Issue, attempt *int) map[string]any {
	blockers := make([]map[string]any, len(issue.BlockedBy))
	for i, b := range issue.BlockedBy {
		blockers[i] = map[string]any{
			"id":         b.ID,
			"identifier": b.Identifier,
			"state":      b.State,
		}
	}

	issueMap := map[string]any{
		"id":          issue.ID,
		"identifier":  issue.Identifier,
		"title":       issue.Title,
		"description": nilIfEmpty(issue.Description),
		"state":       issue.State,
		"branch_name": nilIfEmpty(issue.BranchName),
		"url":         nilIfEmpty(issue.URL),
		"labels":      issue.Labels,
		"blocked_by":  blockers,
	}

	if issue.Priority != nil {
		issueMap["priority"] = *issue.Priority
	}
	if issue.CreatedAt != nil {
		issueMap["created_at"] = issue.CreatedAt.Format("2006-01-02T15:04:05Z")
	}
	if issue.UpdatedAt != nil {
		issueMap["updated_at"] = issue.UpdatedAt.Format("2006-01-02T15:04:05Z")
	}

	bindings := map[string]any{
		"issue": issueMap,
	}

	if attempt != nil {
		bindings["attempt"] = *attempt
	}

	return bindings
}

// nilIfEmpty returns nil for empty strings so Liquid treats them as falsy.
func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
