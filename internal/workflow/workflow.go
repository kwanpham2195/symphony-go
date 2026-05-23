// Package workflow loads and parses WORKFLOW.md files.
//
// A workflow file is Markdown with optional YAML front matter delimited by
// "---" lines. The front matter decodes to a map; the remaining body becomes
// the prompt template.
package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kwanpham2195/symphony-go/internal/domain"
	"gopkg.in/yaml.v3"
)

const defaultFileName = "WORKFLOW.md"

// Sentinel errors returned by Load.
var (
	ErrMissingWorkflowFile = errors.New("missing_workflow_file")
	ErrWorkflowParseError  = errors.New("workflow_parse_error")
	ErrFrontMatterNotAMap  = errors.New("workflow_front_matter_not_a_map")
)

// ResolvePath returns the workflow file path. If explicit is non-empty it wins;
// otherwise defaultFileName in cwd is used.
func ResolvePath(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve workflow path: %w", err)
	}
	return filepath.Join(cwd, defaultFileName), nil
}

// Load reads and parses a workflow file at path.
func Load(path string) (*domain.Workflow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrMissingWorkflowFile, path, err)
	}
	return Parse(string(data))
}

// Parse parses raw workflow content into a Workflow.
func Parse(content string) (*domain.Workflow, error) {
	frontMatterLines, promptLines := splitFrontMatter(content)
	config, err := parseFrontMatter(frontMatterLines)
	if err != nil {
		return nil, err
	}
	prompt := strings.TrimSpace(strings.Join(promptLines, "\n"))
	return &domain.Workflow{
		Config:         config,
		PromptTemplate: prompt,
	}, nil
}

// splitFrontMatter splits content into YAML front matter lines and the
// remaining prompt body lines. If the content does not start with "---", the
// entire content is treated as the prompt body.
func splitFrontMatter(content string) (frontMatter []string, body []string) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != "---" {
		return nil, lines
	}

	// Skip opening "---"
	rest := lines[1:]
	var fm []string
	for i, line := range rest {
		if strings.TrimRight(line, "\r") == "---" {
			return fm, rest[i+1:]
		}
		fm = append(fm, line)
	}
	// No closing "---": treat everything after opening as front matter,
	// no prompt body.
	return fm, nil
}

// parseFrontMatter decodes YAML lines into a map. Empty front matter returns
// an empty map.
func parseFrontMatter(lines []string) (map[string]any, error) {
	raw := strings.TrimSpace(strings.Join(lines, "\n"))
	if raw == "" {
		return map[string]any{}, nil
	}

	var decoded any
	if err := yaml.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrWorkflowParseError, err)
	}

	m, ok := decoded.(map[string]any)
	if !ok {
		return nil, ErrFrontMatterNotAMap
	}

	// Normalize map keys to strings recursively (yaml.v3 returns
	// map[string]any by default, but be defensive).
	return normalizeMap(m), nil
}

// normalizeMap recursively ensures all map keys are strings.
func normalizeMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = normalizeValue(v)
	}
	return out
}

func normalizeValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return normalizeMap(t)
	case map[any]any:
		out := make(map[string]any, len(t))
		for key, val := range t {
			out[fmt.Sprintf("%v", key)] = normalizeValue(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = normalizeValue(val)
		}
		return out
	default:
		return v
	}
}
