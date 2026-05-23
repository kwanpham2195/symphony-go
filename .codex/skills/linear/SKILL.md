---
name: linear
description: |
  Use Symphony's linear_graphql client tool for raw Linear GraphQL
  operations such as comment editing, state transitions, and uploads.
---

# Linear GraphQL

Use the `linear_graphql` tool exposed by Symphony's app-server session for raw Linear API access.

## Tool Input

```json
{
  "query": "query or mutation document",
  "variables": { "optional": "graphql variables" }
}
```

One operation per call. Top-level `errors` array means the operation failed.

## Common Operations

### Query an issue

```graphql
query IssueByKey($key: String!) {
  issue(id: $key) {
    id identifier title
    state { id name }
    project { id name }
    url description
  }
}
```

### Get team workflow states

```graphql
query IssueTeamStates($id: String!) {
  issue(id: $id) {
    team {
      states { nodes { id name type } }
    }
  }
}
```

### Move issue to a state

```graphql
mutation MoveIssue($id: String!, $stateId: String!) {
  issueUpdate(id: $id, input: { stateId: $stateId }) {
    success
    issue { id identifier state { name } }
  }
}
```

### Create a comment

```graphql
mutation CreateComment($issueId: String!, $body: String!) {
  commentCreate(input: { issueId: $issueId, body: $body }) {
    success
    comment { id url }
  }
}
```

### Update a comment

```graphql
mutation UpdateComment($id: String!, $body: String!) {
  commentUpdate(id: $id, input: { body: $body }) {
    success
    comment { id body }
  }
}
```

### Attach a GitHub PR

```graphql
mutation AttachPR($issueId: String!, $url: String!, $title: String) {
  attachmentLinkGitHubPR(issueId: $issueId, url: $url, title: $title, linkKind: links) {
    success
    attachment { id url }
  }
}
```

### Schema introspection

```graphql
query ListMutations {
  __type(name: "Mutation") { fields { name } }
}
```

```graphql
query InspectInput($name: String!) {
  __type(name: $name) {
    inputFields { name type { kind name ofType { kind name } } }
  }
}
```

## Rules

- Use `linear_graphql` for all Linear API access. Do not use raw tokens in shell commands.
- Fetch team states before state transitions — use the exact `stateId`.
- Prefer the narrowest issue lookup: key → identifier search → internal id.
- Prefer `attachmentLinkGitHubPR` over generic URL attachment for PRs.
