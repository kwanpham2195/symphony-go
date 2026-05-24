// Package tools implements dynamic app-server tools that Symphony advertises
// to the Codex session.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// GraphQLExecutor executes a raw GraphQL query against Linear.
type GraphQLExecutor interface {
	ExecuteGraphQL(ctx context.Context, query string, variables map[string]any) (json.RawMessage, error)
}

// LinearGraphQL is the linear_graphql client-side tool.
type LinearGraphQL struct {
	executor GraphQLExecutor
}

// NewLinearGraphQL creates a linear_graphql tool.
func NewLinearGraphQL(executor GraphQLExecutor) *LinearGraphQL {
	return &LinearGraphQL{executor: executor}
}

// Name returns the tool name.
func (t *LinearGraphQL) Name() string {
	return "linear_graphql"
}

// Spec returns the tool specification for advertising to the app-server.
func (t *LinearGraphQL) Spec() map[string]any {
	return map[string]any{
		"name":        "linear_graphql",
		"description": "Execute a raw GraphQL query or mutation against Linear using Symphony's configured auth.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "A single GraphQL query or mutation document.",
				},
				"variables": map[string]any{
					"type":        "object",
					"description": "Optional GraphQL variables object.",
				},
			},
			"required": []string{"query"},
		},
	}
}

// Execute runs the linear_graphql tool.
func (t *LinearGraphQL) Execute(ctx context.Context, args any) ToolResult {
	// Parse arguments
	query, variables, err := parseToolArgs(args)
	if err != nil {
		return failResult(fmt.Sprintf("invalid arguments: %v", err))
	}

	// Validate query
	if strings.TrimSpace(query) == "" {
		return failResult("query must be a non-empty string")
	}

	// Check for multiple operations (basic check)
	if countOperations(query) > 1 {
		return failResult("query must contain exactly one GraphQL operation")
	}

	// Execute
	body, err := t.executor.ExecuteGraphQL(ctx, query, variables)
	if err != nil {
		return failResult(fmt.Sprintf("GraphQL execution failed: %v", err))
	}

	// Check for GraphQL errors in response
	var resp struct {
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(body, &resp) == nil && len(resp.Errors) > 0 {
		return ToolResult{
			Success:      false,
			Output:       string(body),
			ContentItems: []ContentItem{{Type: "inputText", Text: string(body)}},
		}
	}

	return ToolResult{
		Success:      true,
		Output:       string(body),
		ContentItems: []ContentItem{{Type: "inputText", Text: string(body)}},
	}
}

// --- helpers ---

func parseToolArgs(args any) (string, map[string]any, error) {
	switch a := args.(type) {
	case string:
		// Raw query string shorthand
		return a, nil, nil
	case map[string]any:
		query, _ := a["query"].(string)
		if query == "" {
			return "", nil, fmt.Errorf("missing 'query' field")
		}
		var variables map[string]any
		if v, ok := a["variables"]; ok {
			vars, ok := v.(map[string]any)
			if !ok {
				return "", nil, fmt.Errorf("'variables' must be a JSON object")
			}
			variables = vars
		}
		return query, variables, nil
	default:
		return "", nil, fmt.Errorf("expected object with 'query' field or a query string")
	}
}

// countOperations does a basic count of GraphQL operations in a query string.
// Looks for "query", "mutation", "subscription" keywords at the top level.
func countOperations(query string) int {
	count := 0
	lower := strings.ToLower(query)
	// Simple heuristic: count top-level operation keywords
	for _, kw := range []string{"query ", "mutation ", "subscription "} {
		idx := 0
		for {
			pos := strings.Index(lower[idx:], kw)
			if pos < 0 {
				break
			}
			// Check it's not inside a string (basic: not preceded by quote)
			absPos := idx + pos
			if absPos == 0 || (lower[absPos-1] != '"' && lower[absPos-1] != '\'') {
				count++
			}
			idx = absPos + len(kw)
		}
	}
	if count == 0 {
		// Could be a shorthand query (just { ... })
		trimmed := strings.TrimSpace(query)
		if strings.HasPrefix(trimmed, "{") {
			count = 1
		}
	}
	return count
}

func failResult(msg string) ToolResult {
	return ToolResult{
		Success:      false,
		Output:       msg,
		ContentItems: []ContentItem{{Type: "inputText", Text: msg}},
	}
}
