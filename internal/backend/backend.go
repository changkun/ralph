package backend

import (
	"context"
	"encoding/json"

	"github.com/changkun/ralph/internal/prompt"
)

// Backend runs LLM commands for Ralph's orchestration loops.
type Backend interface {
	RunThinker(ctx context.Context, folder string, p prompt.Prompt) ([]byte, error)
	RunWorker(ctx context.Context, folder string, p prompt.Prompt) ([]byte, error)
	RunArchivist(ctx context.Context, folder string, p prompt.Prompt) ([]byte, error)
}

// Result is the JSON structure returned by LLM backends.
type Result struct {
	Value string `json:"result"`
}

// ExtractResult parses the JSON output and returns the result string.
func ExtractResult(raw []byte) string {
	var r Result
	if err := json.Unmarshal(raw, &r); err != nil {
		return ""
	}
	return r.Value
}

// New creates a Backend by name ("claude" or "codex").
func New(name string) Backend {
	switch name {
	case "claude":
		return &Claude{}
	case "codex":
		return &Codex{}
	}
	return nil
}
