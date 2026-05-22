package linear

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

// loadFixture reads a JSON fixture file from testdata/.
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("load fixture %s: %v", name, err)
	}
	return data
}

// fakeTransport returns a DoRequest func that serves canned responses.
// requestLog receives each request body for assertion.
type fakeResponse struct {
	statusCode int
	body       []byte
}

func fakeDoRequest(responses []fakeResponse, requestLog *[]map[string]any) func(*http.Request) (*http.Response, error) {
	callIndex := 0
	return func(req *http.Request) (*http.Response, error) {
		if requestLog != nil {
			bodyBytes, _ := io.ReadAll(req.Body)
			var parsed map[string]any
			_ = json.Unmarshal(bodyBytes, &parsed)
			*requestLog = append(*requestLog, parsed)
		}

		if callIndex >= len(responses) {
			return &http.Response{
				StatusCode: 500,
				Body:       io.NopCloser(strings.NewReader(`{"error":"no more responses"}`)),
			}, nil
		}
		resp := responses[callIndex]
		callIndex++
		return &http.Response{
			StatusCode: resp.statusCode,
			Body:       io.NopCloser(strings.NewReader(string(resp.body))),
		}, nil
	}
}

func newTestClient(doReq func(*http.Request) (*http.Response, error)) *Client {
	c := NewClient("https://api.linear.app/graphql", "test-token", "test-project", []string{"Todo", "In Progress"})
	c.DoRequest = doReq
	return c
}

// --- Normalization tests ---

func TestNormalizeIssue_FullPayload(t *testing.T) {
	data := loadFixture(t, "candidate_issues.json")
	var resp graphQLResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatal(err)
	}

	nodes := resp.Data.Issues.Nodes
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}

	issue1 := normalizeIssue(nodes[0])
	if issue1.ID != "issue-1" {
		t.Errorf("id = %q", issue1.ID)
	}
	if issue1.Identifier != "SYM-1" {
		t.Errorf("identifier = %q", issue1.Identifier)
	}
	if issue1.Title != "First issue" {
		t.Errorf("title = %q", issue1.Title)
	}
	if issue1.Description != "Fix the bug" {
		t.Errorf("description = %q", issue1.Description)
	}
	if issue1.Priority == nil || *issue1.Priority != 1 {
		t.Errorf("priority = %v", issue1.Priority)
	}
	if issue1.State != "Todo" {
		t.Errorf("state = %q", issue1.State)
	}
	if issue1.BranchName != "sym-1-fix-bug" {
		t.Errorf("branch_name = %q", issue1.BranchName)
	}

	// Labels should be lowercase
	if len(issue1.Labels) != 2 {
		t.Fatalf("labels len = %d", len(issue1.Labels))
	}
	if issue1.Labels[0] != "bug" {
		t.Errorf("label[0] = %q, want bug", issue1.Labels[0])
	}
	if issue1.Labels[1] != "urgent" {
		t.Errorf("label[1] = %q, want urgent", issue1.Labels[1])
	}

	// Timestamps
	if issue1.CreatedAt == nil {
		t.Error("created_at nil")
	}
	if issue1.UpdatedAt == nil {
		t.Error("updated_at nil")
	}

	// No blockers
	if len(issue1.BlockedBy) != 0 {
		t.Errorf("blockers = %v", issue1.BlockedBy)
	}
}

func TestNormalizeIssue_Blockers(t *testing.T) {
	data := loadFixture(t, "candidate_issues.json")
	var resp graphQLResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatal(err)
	}

	issue2 := normalizeIssue(resp.Data.Issues.Nodes[1])
	if len(issue2.BlockedBy) != 1 {
		t.Fatalf("expected 1 blocker, got %d", len(issue2.BlockedBy))
	}
	blocker := issue2.BlockedBy[0]
	if blocker.ID != "blocker-1" {
		t.Errorf("blocker.id = %q", blocker.ID)
	}
	if blocker.Identifier != "SYM-10" {
		t.Errorf("blocker.identifier = %q", blocker.Identifier)
	}
	if blocker.State != "In Progress" {
		t.Errorf("blocker.state = %q", blocker.State)
	}
}

func TestNormalizeIssue_NullFields(t *testing.T) {
	data := loadFixture(t, "candidate_issues.json")
	var resp graphQLResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatal(err)
	}

	issue2 := normalizeIssue(resp.Data.Issues.Nodes[1])
	if issue2.Description != "" {
		t.Errorf("description should be empty for null, got %q", issue2.Description)
	}
	if issue2.BranchName != "" {
		t.Errorf("branch_name should be empty for null, got %q", issue2.BranchName)
	}
}

func TestNormalizeIssue_PriorityNonInteger(t *testing.T) {
	raw := rawIssue{
		ID:    "test",
		Title: "Test",
		State: &stateNode{Name: "Todo"},
	}
	issue := normalizeIssue(raw)
	if issue.Priority != nil {
		t.Errorf("priority should be nil for missing, got %v", issue.Priority)
	}
}

// --- FetchCandidateIssues tests ---

func TestFetchCandidateIssues_SinglePage(t *testing.T) {
	data := loadFixture(t, "candidate_issues.json")
	c := newTestClient(fakeDoRequest([]fakeResponse{{200, data}}, nil))

	issues, err := c.FetchCandidateIssuesWithStates(context.Background(), []string{"Todo", "In Progress"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2, got %d", len(issues))
	}
	if issues[0].Identifier != "SYM-1" {
		t.Errorf("first issue = %q", issues[0].Identifier)
	}
}

func TestFetchCandidateIssues_Pagination(t *testing.T) {
	page1 := loadFixture(t, "page1.json")
	page2 := loadFixture(t, "page2.json")

	var log []map[string]any
	c := newTestClient(fakeDoRequest([]fakeResponse{
		{200, page1},
		{200, page2},
	}, &log))

	issues, err := c.FetchCandidateIssuesWithStates(context.Background(), []string{"Todo", "In Progress"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues across pages, got %d", len(issues))
	}
	if issues[0].Identifier != "SYM-1" {
		t.Errorf("first = %q", issues[0].Identifier)
	}
	if issues[1].Identifier != "SYM-2" {
		t.Errorf("second = %q", issues[1].Identifier)
	}

	// Second request should have the cursor
	if len(log) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(log))
	}
	vars, _ := log[1]["variables"].(map[string]any)
	if vars["after"] != "cursor-page-2" {
		t.Errorf("second request cursor = %v", vars["after"])
	}
}

func TestFetchCandidateIssues_GraphQLError(t *testing.T) {
	data := loadFixture(t, "graphql_error.json")
	c := newTestClient(fakeDoRequest([]fakeResponse{{200, data}}, nil))

	_, err := c.FetchCandidateIssuesWithStates(context.Background(), []string{"Todo"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "linear_graphql_errors") {
		t.Errorf("expected graphql error, got: %v", err)
	}
}

func TestFetchCandidateIssues_NonOKStatus(t *testing.T) {
	c := newTestClient(fakeDoRequest([]fakeResponse{
		{401, []byte(`{"error":"unauthorized"}`)},
	}, nil))

	_, err := c.FetchCandidateIssuesWithStates(context.Background(), []string{"Todo"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "linear_api_status") {
		t.Errorf("expected api status error, got: %v", err)
	}
}

func TestFetchCandidateIssues_MalformedPayload(t *testing.T) {
	data := loadFixture(t, "malformed.json")
	c := newTestClient(fakeDoRequest([]fakeResponse{{200, data}}, nil))

	_, err := c.FetchCandidateIssuesWithStates(context.Background(), []string{"Todo"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "linear_unknown_payload") {
		t.Errorf("expected unknown payload error, got: %v", err)
	}
}

// --- FetchIssueStatesByIDs tests ---

func TestFetchIssueStatesByIDs_Basic(t *testing.T) {
	data := loadFixture(t, "by_ids.json")
	var log []map[string]any
	c := newTestClient(fakeDoRequest([]fakeResponse{{200, data}}, &log))

	issues, err := c.FetchIssueStatesByIDs(context.Background(), []string{"issue-2", "issue-1"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2, got %d", len(issues))
	}
	// Should be in requested order
	if issues[0].ID != "issue-2" {
		t.Errorf("first should be issue-2, got %q", issues[0].ID)
	}
	if issues[1].ID != "issue-1" {
		t.Errorf("second should be issue-1, got %q", issues[1].ID)
	}

	// Verify query used IDs variable
	vars, _ := log[0]["variables"].(map[string]any)
	ids, _ := vars["ids"].([]any)
	if len(ids) != 2 {
		t.Errorf("expected 2 ids in query, got %d", len(ids))
	}
}

func TestFetchIssueStatesByIDs_Empty(t *testing.T) {
	c := newTestClient(fakeDoRequest(nil, nil))
	issues, err := c.FetchIssueStatesByIDs(context.Background(), nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("expected 0, got %d", len(issues))
	}
}

// --- FetchIssuesByStates tests ---

func TestFetchIssuesByStates_Empty(t *testing.T) {
	c := newTestClient(fakeDoRequest(nil, nil))
	issues, err := c.FetchIssuesByStates(context.Background(), nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("expected 0, got %d", len(issues))
	}
}

func TestFetchIssuesByStates_Terminal(t *testing.T) {
	data := loadFixture(t, "candidate_issues.json")
	c := newTestClient(fakeDoRequest([]fakeResponse{{200, data}}, nil))

	issues, err := c.FetchIssuesByStates(context.Background(), []string{"Done", "Closed"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(issues) != 2 {
		t.Errorf("expected 2, got %d", len(issues))
	}
}

// --- Request payload tests ---

func TestFetchCandidateIssues_RequestPayload(t *testing.T) {
	data := loadFixture(t, "candidate_issues.json")
	var log []map[string]any
	c := newTestClient(fakeDoRequest([]fakeResponse{{200, data}}, &log))
	c.ProjectSlug = "my-project"

	_, err := c.FetchCandidateIssuesWithStates(context.Background(), []string{"Todo", "In Progress"})
	if err != nil {
		t.Fatal(err)
	}

	if len(log) != 1 {
		t.Fatalf("expected 1 request, got %d", len(log))
	}

	vars, _ := log[0]["variables"].(map[string]any)
	if vars["projectSlug"] != "my-project" {
		t.Errorf("projectSlug = %v", vars["projectSlug"])
	}
	states, _ := vars["stateNames"].([]any)
	if len(states) != 2 {
		t.Errorf("stateNames = %v", states)
	}
	first, _ := vars["first"].(float64)
	if int(first) != issuePageSize {
		t.Errorf("first = %v", first)
	}
}

// --- Auth header test ---

func TestFetchCandidateIssues_AuthHeader(t *testing.T) {
	data := loadFixture(t, "candidate_issues.json")
	var capturedAuth string
	c := newTestClient(func(req *http.Request) (*http.Response, error) {
		capturedAuth = req.Header.Get("Authorization")
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(string(data))),
		}, nil
	})
	c.APIKey = "lin_api_secret"

	_, _ = c.FetchCandidateIssuesWithStates(context.Background(), []string{"Todo"})
	if capturedAuth != "lin_api_secret" {
		t.Errorf("Authorization = %q", capturedAuth)
	}
}

// --- Pagination edge case: hasNextPage=true but no cursor ---

func TestPagination_MissingCursor(t *testing.T) {
	fixture := `{
		"data": {
			"issues": {
				"nodes": [],
				"pageInfo": { "hasNextPage": true, "endCursor": null }
			}
		}
	}`
	c := newTestClient(fakeDoRequest([]fakeResponse{{200, []byte(fixture)}}, nil))

	_, err := c.FetchCandidateIssuesWithStates(context.Background(), []string{"Todo"})
	if err == nil {
		t.Fatal("expected error for missing cursor")
	}
	if !strings.Contains(err.Error(), "linear_missing_end_cursor") {
		t.Errorf("expected missing cursor error, got: %v", err)
	}
}
