package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// --- Fake executor ---

type fakeExecutor struct {
	lastQuery     string
	lastVariables map[string]any
	response      json.RawMessage
	err           error
}

func (f *fakeExecutor) ExecuteGraphQL(_ context.Context, query string, variables map[string]any) (json.RawMessage, error) {
	f.lastQuery = query
	f.lastVariables = variables
	return f.response, f.err
}

// --- Tests ---

func TestLinearGraphQL_Name(t *testing.T) {
	tool := NewLinearGraphQL(&fakeExecutor{})
	if tool.Name() != "linear_graphql" {
		t.Errorf("name = %q", tool.Name())
	}
}

func TestLinearGraphQL_Spec(t *testing.T) {
	tool := NewLinearGraphQL(&fakeExecutor{})
	spec := tool.Spec()
	if spec["name"] != "linear_graphql" {
		t.Errorf("spec name = %v", spec["name"])
	}
}

func TestLinearGraphQL_SuccessfulQuery(t *testing.T) {
	exec := &fakeExecutor{
		response: json.RawMessage(`{"data":{"viewer":{"id":"user-1"}}}`),
	}
	tool := NewLinearGraphQL(exec)

	result := tool.Execute(context.Background(), map[string]any{
		"query": "query { viewer { id } }",
	})

	if !result.Success {
		t.Errorf("expected success, got: %s", result.Output)
	}
	if exec.lastQuery != "query { viewer { id } }" {
		t.Errorf("query = %q", exec.lastQuery)
	}
	if !strings.Contains(result.Output, "viewer") {
		t.Errorf("output = %q", result.Output)
	}
}

func TestLinearGraphQL_WithVariables(t *testing.T) {
	exec := &fakeExecutor{
		response: json.RawMessage(`{"data":{"issue":{"id":"issue-1"}}}`),
	}
	tool := NewLinearGraphQL(exec)

	result := tool.Execute(context.Background(), map[string]any{
		"query": "query GetIssue($id: ID!) { issue(id: $id) { id } }",
		"variables": map[string]any{
			"id": "issue-1",
		},
	})

	if !result.Success {
		t.Errorf("expected success")
	}
	if exec.lastVariables["id"] != "issue-1" {
		t.Errorf("variables = %v", exec.lastVariables)
	}
}

func TestLinearGraphQL_RawQueryString(t *testing.T) {
	exec := &fakeExecutor{
		response: json.RawMessage(`{"data":{}}`),
	}
	tool := NewLinearGraphQL(exec)

	result := tool.Execute(context.Background(), "{ viewer { id } }")

	if !result.Success {
		t.Errorf("expected success for raw query string")
	}
	if exec.lastQuery != "{ viewer { id } }" {
		t.Errorf("query = %q", exec.lastQuery)
	}
}

func TestLinearGraphQL_GraphQLErrors(t *testing.T) {
	exec := &fakeExecutor{
		response: json.RawMessage(`{"errors":[{"message":"Unauthorized"}]}`),
	}
	tool := NewLinearGraphQL(exec)

	result := tool.Execute(context.Background(), map[string]any{
		"query": "query { viewer { id } }",
	})

	if result.Success {
		t.Error("expected failure for GraphQL errors")
	}
	if !strings.Contains(result.Output, "Unauthorized") {
		t.Errorf("output should contain error, got: %s", result.Output)
	}
}

func TestLinearGraphQL_TransportError(t *testing.T) {
	exec := &fakeExecutor{
		err: fmt.Errorf("network timeout"),
	}
	tool := NewLinearGraphQL(exec)

	result := tool.Execute(context.Background(), map[string]any{
		"query": "query { viewer { id } }",
	})

	if result.Success {
		t.Error("expected failure for transport error")
	}
	if !strings.Contains(result.Output, "network timeout") {
		t.Errorf("output = %q", result.Output)
	}
}

func TestLinearGraphQL_BlankQuery(t *testing.T) {
	tool := NewLinearGraphQL(&fakeExecutor{})

	result := tool.Execute(context.Background(), map[string]any{
		"query": "   ",
	})

	if result.Success {
		t.Error("expected failure for blank query")
	}
	if !strings.Contains(result.Output, "non-empty") {
		t.Errorf("output = %q", result.Output)
	}
}

func TestLinearGraphQL_MissingQuery(t *testing.T) {
	tool := NewLinearGraphQL(&fakeExecutor{})

	result := tool.Execute(context.Background(), map[string]any{
		"variables": map[string]any{},
	})

	if result.Success {
		t.Error("expected failure for missing query")
	}
}

func TestLinearGraphQL_InvalidVariables(t *testing.T) {
	tool := NewLinearGraphQL(&fakeExecutor{})

	result := tool.Execute(context.Background(), map[string]any{
		"query":     "query { viewer { id } }",
		"variables": "not-an-object",
	})

	if result.Success {
		t.Error("expected failure for invalid variables")
	}
	if !strings.Contains(result.Output, "JSON object") {
		t.Errorf("output = %q", result.Output)
	}
}

func TestLinearGraphQL_MultipleOperations(t *testing.T) {
	tool := NewLinearGraphQL(&fakeExecutor{})

	result := tool.Execute(context.Background(), map[string]any{
		"query": "query A { viewer { id } } query B { issues { nodes { id } } }",
	})

	if result.Success {
		t.Error("expected failure for multiple operations")
	}
	if !strings.Contains(result.Output, "exactly one") {
		t.Errorf("output = %q", result.Output)
	}
}

func TestLinearGraphQL_InvalidArgs(t *testing.T) {
	tool := NewLinearGraphQL(&fakeExecutor{})

	result := tool.Execute(context.Background(), 42)

	if result.Success {
		t.Error("expected failure for invalid args type")
	}
}

func TestLinearGraphQL_ContentItems(t *testing.T) {
	exec := &fakeExecutor{
		response: json.RawMessage(`{"data":{"ok":true}}`),
	}
	tool := NewLinearGraphQL(exec)

	result := tool.Execute(context.Background(), map[string]any{
		"query": "query { ok }",
	})

	if len(result.ContentItems) != 1 {
		t.Fatalf("contentItems len = %d", len(result.ContentItems))
	}
	if result.ContentItems[0].Type != "inputText" {
		t.Errorf("type = %q", result.ContentItems[0].Type)
	}
}
