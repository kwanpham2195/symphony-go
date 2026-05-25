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

	"github.com/kwanpham2195/symphony-go/internal"
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
	Endpoint     string
	APIKey       string
	ProjectSlug  string
	ActiveStates []string
	HTTPClient   *http.Client

	// DoRequest is an optional override for HTTP request execution.
	// Used in tests to inject fake responses.
	DoRequest func(req *http.Request) (*http.Response, error)
}

// NewClient creates a Linear client from config values.
func NewClient(endpoint, apiKey, projectSlug string, activeStates []string) *Client {
	return &Client{
		Endpoint:     endpoint,
		APIKey:       apiKey,
		ProjectSlug:  projectSlug,
		ActiveStates: activeStates,
		HTTPClient: &http.Client{
			Timeout: networkTimeout,
		},
	}
}

// FetchCandidateIssues returns issues in active states for the project.
func (c *Client) FetchCandidateIssues(ctx context.Context) ([]internal.Issue, error) {
	return c.fetchByStates(ctx, c.ActiveStates)
}

// FetchCandidateIssuesWithStates returns issues in the given states for the
// configured project, with pagination.
func (c *Client) FetchCandidateIssuesWithStates(ctx context.Context, states []string) ([]internal.Issue, error) {
	return c.fetchByStates(ctx, states)
}

// FetchIssuesByStates returns issues in the given states for the configured
// project.
func (c *Client) FetchIssuesByStates(ctx context.Context, states []string) ([]internal.Issue, error) {
	if len(states) == 0 {
		return nil, nil
	}
	return c.fetchByStates(ctx, states)
}

// FetchIssueStatesByIDs returns current issue data for specific IDs.
func (c *Client) FetchIssueStatesByIDs(ctx context.Context, ids []string) ([]internal.Issue, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// Build order index for stable output
	orderIndex := make(map[string]int, len(ids))
	for i, id := range ids {
		orderIndex[id] = i
	}

	var all []internal.Issue
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

func (c *Client) fetchByStates(ctx context.Context, states []string) ([]internal.Issue, error) {
	var all []internal.Issue
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
	defer resp.Body.Close() //nolint:errcheck // best-effort close

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
	ID               string         `json:"id"`
	Identifier       string         `json:"identifier"`
	Title            string         `json:"title"`
	Description      *string        `json:"description"`
	Priority         *int           `json:"priority"`
	State            *stateNode     `json:"state"`
	BranchName       *string        `json:"branchName"`
	URL              *string        `json:"url"`
	Labels           *labelsWrapper `json:"labels"`
	InverseRelations *relationsWrap `json:"inverseRelations"`
	CreatedAt        *string        `json:"createdAt"`
	UpdatedAt        *string        `json:"updatedAt"`
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

func decodeIssuesResponse(body json.RawMessage) ([]internal.Issue, error) {
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
	issues := make([]internal.Issue, 0, len(resp.Data.Issues.Nodes))
	for _, raw := range resp.Data.Issues.Nodes {
		issue := normalizeIssue(raw)
		if issue != nil {
			issues = append(issues, *issue)
		}
	}
	return issues, nil
}

func decodePagedResponse(body json.RawMessage) ([]internal.Issue, *pageInfo, error) {
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
	issues := make([]internal.Issue, 0, len(resp.Data.Issues.Nodes))
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
func NormalizeIssue(raw json.RawMessage) (*internal.Issue, error) {
	var ri rawIssue
	if err := json.Unmarshal(raw, &ri); err != nil {
		return nil, err
	}
	return normalizeIssue(ri), nil
}

func normalizeIssue(raw rawIssue) *internal.Issue {
	issue := &internal.Issue{
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
				blocker := internal.Blocker{
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

func sortByIndex(issues []internal.Issue, orderIndex map[string]int) {
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

// --- comment queries ---

const commentsQuery = `
query SymphonyComments($issueIDs: [ID!]!, $since: DateTime!, $first: Int!) {
  issues(filter: {id: {in: $issueIDs}}, first: $first) {
    nodes {
      id
      comments(filter: {createdAt: {gt: $since}}, first: 20, orderBy: createdAt) {
        nodes {
          id
          body
          createdAt
          user { id name }
          botActor { id name }
          parent { id }
        }
      }
    }
  }
}
`

// FetchRecentComments returns comments created after since for the given
// issue IDs. The returned map is keyed by issue ID.
func (c *Client) FetchRecentComments(ctx context.Context, issueIDs []string, since time.Time) (map[string][]internal.Comment, error) {
	if len(issueIDs) == 0 {
		return nil, nil
	}

	result := make(map[string][]internal.Comment)

	for i := 0; i < len(issueIDs); i += issuePageSize {
		end := i + issuePageSize
		if end > len(issueIDs) {
			end = len(issueIDs)
		}
		batch := issueIDs[i:end]

		vars := map[string]any{
			"issueIDs": batch,
			"since":    since.Format(time.RFC3339),
			"first":    len(batch),
		}

		body, err := c.doGraphQL(ctx, commentsQuery, vars)
		if err != nil {
			return nil, err
		}

		parsed, err := decodeCommentsResponse(body)
		if err != nil {
			return nil, err
		}
		for k, v := range parsed {
			result[k] = append(result[k], v...)
		}
	}

	return result, nil
}

type commentsGraphQLResponse struct {
	Data   *commentsData  `json:"data"`
	Errors []graphQLError `json:"errors"`
}

type commentsData struct {
	Issues commentsIssueContainer `json:"issues"`
}

type commentsIssueContainer struct {
	Nodes []commentsIssueNode `json:"nodes"`
}

type commentsIssueNode struct {
	ID       string          `json:"id"`
	Comments commentsWrapper `json:"comments"`
}

type commentsWrapper struct {
	Nodes []rawComment `json:"nodes"`
}

type rawComment struct {
	ID        string       `json:"id"`
	Body      string       `json:"body"`
	CreatedAt string       `json:"createdAt"`
	User      *commentUser `json:"user"`
	BotActor  *commentBot  `json:"botActor"`
	Parent    *commentRef  `json:"parent"`
}

type commentUser struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type commentBot struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type commentRef struct {
	ID string `json:"id"`
}

func decodeCommentsResponse(body json.RawMessage) (map[string][]internal.Comment, error) {
	var resp commentsGraphQLResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("linear_comments_decode: %w", err)
	}
	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("linear_graphql_errors: %s", resp.Errors[0].Message)
	}
	if resp.Data == nil {
		return nil, fmt.Errorf("linear_comments_decode: no data field")
	}

	result := make(map[string][]internal.Comment)
	for _, issue := range resp.Data.Issues.Nodes {
		for _, raw := range issue.Comments.Nodes {
			comment := internal.Comment{
				ID:      raw.ID,
				Body:    raw.Body,
				IssueID: issue.ID,
			}
			if raw.User != nil {
				comment.UserID = raw.User.ID
				comment.UserName = raw.User.Name
			}
			if raw.BotActor != nil {
				comment.BotActor = true
			}
			if raw.Parent != nil {
				comment.ParentID = raw.Parent.ID
			}
			if t, err := time.Parse(time.RFC3339, raw.CreatedAt); err == nil {
				comment.CreatedAt = t
			}
			result[issue.ID] = append(result[issue.ID], comment)
		}
	}

	return result, nil
}

// --- TrackerWriter implementation ---

const viewerQuery = `query { viewer { id } }`

// ViewerID returns the user ID of the authenticated API key owner.
func (c *Client) ViewerID(ctx context.Context) (string, error) {
	body, err := c.doGraphQL(ctx, viewerQuery, nil)
	if err != nil {
		return "", err
	}

	var resp struct {
		Data *struct {
			Viewer struct {
				ID string `json:"id"`
			} `json:"viewer"`
		} `json:"data"`
		Errors []graphQLError `json:"errors"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("linear_viewer_decode: %w", err)
	}
	if len(resp.Errors) > 0 {
		return "", fmt.Errorf("linear_graphql_errors: %s", resp.Errors[0].Message)
	}
	if resp.Data == nil {
		return "", fmt.Errorf("linear_viewer_decode: no data field")
	}
	return resp.Data.Viewer.ID, nil
}

const teamStatesQuery = `
query SymphonyTeamStates($projectSlug: String!) {
  issues(filter: {project: {slugId: {eq: $projectSlug}}}, first: 1) {
    nodes {
      team {
        states { nodes { id name } }
      }
    }
  }
}
`

// ResolveStateID maps a state name to a Linear workflow state UUID by
// querying the team associated with the configured project.
func (c *Client) ResolveStateID(ctx context.Context, stateName string) (string, error) {
	vars := map[string]any{"projectSlug": c.ProjectSlug}
	body, err := c.doGraphQL(ctx, teamStatesQuery, vars)
	if err != nil {
		return "", err
	}

	var resp struct {
		Data *struct {
			Issues struct {
				Nodes []struct {
					Team struct {
						States struct {
							Nodes []struct {
								ID   string `json:"id"`
								Name string `json:"name"`
							} `json:"nodes"`
						} `json:"states"`
					} `json:"team"`
				} `json:"nodes"`
			} `json:"issues"`
		} `json:"data"`
		Errors []graphQLError `json:"errors"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("linear_states_decode: %w", err)
	}
	if len(resp.Errors) > 0 {
		return "", fmt.Errorf("linear_graphql_errors: %s", resp.Errors[0].Message)
	}
	if resp.Data == nil || len(resp.Data.Issues.Nodes) == 0 {
		return "", fmt.Errorf("linear_resolve_state: no issues found for project %q", c.ProjectSlug)
	}

	target := strings.ToLower(strings.TrimSpace(stateName))
	for _, s := range resp.Data.Issues.Nodes[0].Team.States.Nodes {
		if strings.ToLower(strings.TrimSpace(s.Name)) == target {
			return s.ID, nil
		}
	}
	return "", fmt.Errorf("linear_resolve_state: state %q not found", stateName)
}

const transitionMutation = `
mutation SymphonyTransition($id: String!, $stateId: String!) {
  issueUpdate(id: $id, input: {stateId: $stateId}) {
    success
    issue { id state { name } }
  }
}
`

// TransitionIssueState moves an issue to the given state ID.
func (c *Client) TransitionIssueState(ctx context.Context, issueID string, stateID string) error {
	vars := map[string]any{
		"id":      issueID,
		"stateId": stateID,
	}
	body, err := c.doGraphQL(ctx, transitionMutation, vars)
	if err != nil {
		return err
	}

	var resp struct {
		Data *struct {
			IssueUpdate struct {
				Success bool `json:"success"`
			} `json:"issueUpdate"`
		} `json:"data"`
		Errors []graphQLError `json:"errors"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("linear_transition_decode: %w", err)
	}
	if len(resp.Errors) > 0 {
		return fmt.Errorf("linear_graphql_errors: %s", resp.Errors[0].Message)
	}
	if resp.Data == nil || !resp.Data.IssueUpdate.Success {
		return fmt.Errorf("linear_transition: issueUpdate returned success=false")
	}
	return nil
}
