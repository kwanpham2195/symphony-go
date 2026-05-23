//go:build tools

// Package tools tracks tool dependencies for this repo.
// Run: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint
package tools

import (
	_ "github.com/golangci/golangci-lint/v2/cmd/golangci-lint"
)
