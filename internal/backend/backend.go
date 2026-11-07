package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"

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

// waitDelay bounds how long output waits, once the CLI has exited or been
// killed, for subprocesses started by the agent to release the CLI's output.
const waitDelay = 2 * time.Second

// output runs cmd and returns its standard output. If cmd's context is
// cancelled, the CLI is killed.
func output(cmd *exec.Cmd) ([]byte, error) {
	cmd.WaitDelay = waitDelay
	out, err := cmd.Output()
	if errors.Is(err, exec.ErrWaitDelay) {
		// The CLI succeeded, but a subprocess it started (a server, say)
		// still holds its output open. The CLI's own output is complete.
		err = nil
	}
	return out, err
}

// runError wraps the error from a failed CLI run with the most specific
// diagnostic available: the agent's own message if the CLI reported one,
// otherwise the last line the CLI wrote to stderr.
func runError(cli string, err error, message string) error {
	if message == "" {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			message = lastLine(exitErr.Stderr)
		}
	}
	if message == "" {
		return fmt.Errorf("%s: %w", cli, err)
	}
	return fmt.Errorf("%s: %w: %s", cli, err, message)
}

func lastLine(b []byte) string {
	b = bytes.TrimSpace(b)
	if i := bytes.LastIndexByte(b, '\n'); i >= 0 {
		b = bytes.TrimSpace(b[i+1:])
	}
	return string(b)
}
