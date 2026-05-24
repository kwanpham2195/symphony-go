package internal

// Workflow holds the parsed WORKFLOW.md content.
type Workflow struct {
	Config         map[string]any `json:"config"`
	PromptTemplate string         `json:"prompt_template"`
}
