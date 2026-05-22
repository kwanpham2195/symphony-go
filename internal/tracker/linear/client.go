// Package linear implements the Linear GraphQL tracker adapter.
package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/matthew-opn/symphony-go/internal/domain"
)

const (
	issuePageSize  = 50
	networkTimeout = 30 * time.Second
)

// GraphQL queries matching the upstream Elixir implementation.
const candidateQuery = `
query SymphonyLinearPoll($projectSlug: String!, $stateNames: [String!]!, $first: Int!, $relationFirst: Int!, $after: String) {
  issues(filter: {project: {slugId: {eq: $projectSlug}}, state: {name: {in: $stateNames}}}, first: $first, after: $after) {
    nodes {
      id
      identifier
      title
      description
      priority
      state { name }
      branchName
      url
      labels { nodes { name } }
      inverseRelations(first: $relationFirst) {
        nodes {
          type
          issue {
            id
            identifier
            state { name }
          }
        }
      }
      createdAt
      updatedAt
    }
    pageInfo {
      hasNextPage
      endCursor
    }
  }
}
`

const queryByIDs = `
query SymphonyLinearIssuesById($ids: [ID!]!, $first: Int!, $relationFirst: Int!) {
  issues(filter: {id: {in: $ids}}, first: $first) {
    nodes {
      id
      identifier
      title
      description
      priority
      state { name }
      branchName
      url
      labels { nodes { name } }
      inverseRelations(first: $relationFirst) {
        nodes {
          type
          issue {
            id
            identifier
            state { name }
          }
        }
      }
      createdAt
      updatedAt
    }
  }
}
`

// Client is a Linear GraphQL client.
type Client struct {
	Endpoint    string
	APIKey      string
	ProjectSlug string
	HTTPClient  *http.Client

	// DoRequest is an optional override for HTTP request execution.
	// Used in tests to inject fake responses.
	DoRequest func(req *http.Request) (*http.Response, error)
}

// NewClient creates a Linear client from config values.
func NewClient(endpoint, apiKey, projectSlug string) *Client {
	return &Client{
		Endpoint:    endpoint,
		APIKey:      apiKey,
		ProjectSlug: projectSlug,
		HTTPClient: &http.Client{
			Timeout: networkTimeout,
		},
	}
}

// FetchCandidateIssues returns issues in active states for the project.
func (c *Client) FetchCandidateIssues(ctx context.Context) ([]domain.Issue, error) {
	return c.fetchByStates(ctx, nil) // nil = use default active states from caller
}

// FetchCandidateIssuesWithStates returns issues in the given states for the
// configured project, with pagination.
func (c *Client) FetchCandidateIssuesWithStates(ctx context.Context, states []string) ([]domain.Issue, error) {
	return c.fetchByStates(ctx, states)
}

// FetchIssuesByStates returns issues in the given states for the configured
// project.
func (c *Client) FetchIssuesByStates(ctx context.Context, states []string) ([]domain.Issue, error) {
	if len(states) == 0 {
		return nil, nil
	}
	return c.fetchByStates(ctx, states)
}

// FetchIssueStatesByIDs returns current issue data for specific IDs.
func (c *Client) FetchIssueStatesByIDs(ctx context.Context, ids []string) ([]domain.Issue, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// Build order index for stable output
	orderIndex := make(map[string]int, len(ids))
	for i, id := range ids {
		orderIndex[id] = i
	}

	var all []domain.Issue
	for i := 0; i < len(ids); i += issuePageSize {
		end := i + issuePageSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[i:end]

		vars := map[string]any{
			"ids":           batch,
			"first":         len(batch),
			"relationFirst": issuePageSize,
		}

		body, err := c.doGraphQL(ctx, queryByIDs, vars)
		if err != nil {
			return nil, err
		}

		issues, err := decodeIssuesResponse(body)
		if err != nil {
			return nil, err
		}
		all = append(all, issues...)
	}

	// Sort by requested ID order
	sortByIndex(all, orderIndex)
	return all, nil
}

// ExecuteGraphQL runs a raw GraphQL query. Used by the linear_graphql tool.
func (c *Client) ExecuteGraphQL(ctx context.Context, query string, variables map[string]any) (json.RawMessage, error) {
	return c.doGraphQL(ctx, query, variables)
}

// --- internal ---

func (c *Client) fetchByStates(ctx context.Context, states []string) ([]domain.Issue, error) {
	var all []domain.Issue
	var cursor *string

	for {
		vars := map[string]any{
			"projectSlug":   c.ProjectSlug,
			"stateNames":    states,
			"first":         issuePageSize,
			"relationFirst": issuePageSize,
		}
		if cursor != nil {
			vars["after"] = *cursor
		}

		body, err := c.doGraphQL(ctx, candidateQuery, vars)
		if err != nil {
			return nil, err
		}

		issues, pageInfo, err := decodePagedResponse(body)
		if err != nil {
			return nil, err
		}
		all = append(all, issues...)

		next, err := nextPageCursor(pageInfo)
		if err != nil {
			return nil, err
		}
		if next == nil {
			break
		}
		cursor = next
	}

	return all, nil
}

func (c *Client) doGraphQL(ctx context.Context, query string, variables map[string]any) (json.RawMessage, error) {
	payload := map[string]any{
		"query":     query,
		"variables": variables,
	}
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("linear: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.Endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("linear: create request: %w", err)
	}
	req.Header.Set("Authorization", c.APIKey)
	req.Header.Set("Content-Type", "application/json")

	doFn := c.HTTPClient.Do
	if c.DoRequest != nil {
		doFn = c.DoRequest
	}

	resp, err := doFn(req)
	if err != nil {
		return nil, fmt.Errorf("linear_api_request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("linear_api_request: read body: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("linear_api_status: %d: %s", resp.StatusCode, truncate(string(respBody), 1000))
	}

	return json.RawMessage(respBody), nil
}

// --- response decoding ---

type graphQLResponse struct {
	Data   *issuesData    `json:"data"`
	Errors []graphQLError `json:"errors"`
}

type issuesData struct {
	Issues issuesContainer `json:"issues"`
}

type issuesContainer struct {
	Nodes    []rawIssue `json:"nodes"`
	PageInfo *pageInfo  `json:"pageInfo"`
}

type pageInfo struct {
	HasNextPage bool    `json:"hasNextPage"`
	EndCursor   *string `json:"endCursor"`
}

type graphQLError struct {
	Message string `json:"message"`
}

type rawIssue struct {
	ID               string          `json:"id"`
	Identifier       string          `json:"identifier"`
	Title            string          `json:"title"`
	Description      *string         `json:"description"`
	Priority         *int            `json:"priority"`
	State            *stateNode      `json:"state"`
	BranchName       *string         `json:"branchName"`
	URL              *string         `json:"url"`
	Labels           *labelsWrapper  `json:"labels"`
	InverseRelations *relationsWrap  `json:"inverseRelations"`
	CreatedAt        *string         `json:"createdAt"`
	UpdatedAt        *string         `json:"updatedAt"`
}

type stateNode struct {
	Name string `json:"name"`
}

type labelsWrapper struct {
	Nodes []labelNode `json:"nodes"`
}

type labelNode struct {
	Name string `json:"name"`
}

type relationsWrap struct {
	Nodes []relationNode `json:"nodes"`
}

type relationNode struct {
	Type  string    `json:"type"`
	Issue *rawIssue `json:"issue"`
}

func decodeIssuesResponse(body json.RawMessage) ([]domain.Issue, error) {
	var resp graphQLResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("linear_unknown_payload: %w", err)
	}
	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("linear_graphql_errors: %s", resp.Errors[0].Message)
	}
	if resp.Data == nil {
		return nil, fmt.Errorf("linear_unknown_payload: no data field")
	}
	issues := make([]domain.Issue, 0, len(resp.Data.Issues.Nodes))
	for _, raw := range resp.Data.Issues.Nodes {
		issue := normalizeIssue(raw)
		if issue != nil {
			issues = append(issues, *issue)
		}
	}
	return issues, nil
}

func decodePagedResponse(body json.RawMessage) ([]domain.Issue, *pageInfo, error) {
	var resp graphQLResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, nil, fmt.Errorf("linear_unknown_payload: %w", err)
	}
	if len(resp.Errors) > 0 {
		return nil, nil, fmt.Errorf("linear_graphql_errors: %s", resp.Errors[0].Message)
	}
	if resp.Data == nil {
		return nil, nil, fmt.Errorf("linear_unknown_payload: no data field")
	}
	issues := make([]domain.Issue, 0, len(resp.Data.Issues.Nodes))
	for _, raw := range resp.Data.Issues.Nodes {
		issue := normalizeIssue(raw)
		if issue != nil {
			issues = append(issues, *issue)
		}
	}
	return issues, resp.Data.Issues.PageInfo, nil
}

func nextPageCursor(pi *pageInfo) (*string, error) {
	if pi == nil || !pi.HasNextPage {
		return nil, nil
	}
	if pi.EndCursor == nil || *pi.EndCursor == "" {
		return nil, fmt.Errorf("linear_missing_end_cursor")
	}
	return pi.EndCursor, nil
}

// --- normalization ---

// NormalizeIssue is exported for tests.
func NormalizeIssue(raw json.RawMessage) (*domain.Issue, error) {
	var ri rawIssue
	if err := json.Unmarshal(raw, &ri); err != nil {
		return nil, err
	}
	return normalizeIssue(ri), nil
}

func normalizeIssue(raw rawIssue) *domain.Issue {
	issue := &domain.Issue{
		ID:         raw.ID,
		Identifier: raw.Identifier,
		Title:      raw.Title,
		Priority:   raw.Priority,
	}

	if raw.Description != nil {
		issue.Description = *raw.Description
	}
	if raw.State != nil {
		issue.State = raw.State.Name
	}
	if raw.BranchName != nil {
		issue.BranchName = *raw.BranchName
	}
	if raw.URL != nil {
		issue.URL = *raw.URL
	}

	// Labels: lowercase
	if raw.Labels != nil {
		labels := make([]string, 0, len(raw.Labels.Nodes))
		for _, l := range raw.Labels.Nodes {
			labels = append(labels, strings.ToLower(l.Name))
		}
		issue.Labels = labels
	}

	// Blockers: from inverse relations where type == "blocks"
	if raw.InverseRelations != nil {
		for _, rel := range raw.InverseRelations.Nodes {
			if strings.EqualFold(strings.TrimSpace(rel.Type), "blocks") && rel.Issue != nil {
				blocker := domain.Blocker{
					ID:         rel.Issue.ID,
					Identifier: rel.Issue.Identifier,
				}
				if rel.Issue.State != nil {
					blocker.State = rel.Issue.State.Name
				}
				issue.BlockedBy = append(issue.BlockedBy, blocker)
			}
		}
	}

	// Timestamps
	if raw.CreatedAt != nil {
		if t, err := time.Parse(time.RFC3339, *raw.CreatedAt); err == nil {
			issue.CreatedAt = &t
		}
	}
	if raw.UpdatedAt != nil {
		if t, err := time.Parse(time.RFC3339, *raw.UpdatedAt); err == nil {
			issue.UpdatedAt = &t
		}
	}

	return issue
}

func sortByIndex(issues []domain.Issue, orderIndex map[string]int) {
	fallback := len(orderIndex)
	for i := 1; i < len(issues); i++ {
		for j := i; j > 0; j-- {
			idxJ := fallback
			if idx, ok := orderIndex[issues[j].ID]; ok {
				idxJ = idx
			}
			idxJm1 := fallback
			if idx, ok := orderIndex[issues[j-1].ID]; ok {
				idxJm1 = idx
			}
			if idxJ < idxJm1 {
				issues[j], issues[j-1] = issues[j-1], issues[j]
			} else {
				break
			}
		}
	}
}

func truncate(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen] + "...<truncated>"
	}
	return s
}
