// Package backend runs agent turns through an external coding-agent CLI.
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

// Role identifies the agent role a turn is run for. Its value also names the
// round log file (round-NNN-<role>.json) that Ralph writes for the turn.
type Role string

// Agent roles. Standalone is the only role in the standalone pattern; the
// others make up the strategist pipeline.
const (
	Strategist Role = "strategist"
	Executor   Role = "executor"
	Evaluator  Role = "evaluator"
	Archivist  Role = "archivist"
	Standalone Role = "standalone"
)

// readOnly reports whether role only reasons about the project and so must
// not be able to change it. Only the strategist is read-only.
func (r Role) readOnly() bool { return r == Strategist }

// Backend runs one agent turn at a time.
type Backend interface {
	// Run runs one turn of role in folder and returns JSON whose "result"
	// field holds the agent's final message (see ExtractResult). If the CLI
	// fails, the error says why.
	Run(ctx context.Context, role Role, folder string, p prompt.Prompt) ([]byte, error)
}

// New returns the backend with the given name ("claude" or "codex"), or nil
// if there is none.
func New(name string) Backend {
	switch name {
	case "claude":
		return &Claude{}
	case "codex":
		return &Codex{}
	}
	return nil
}

// Result is the JSON shape Backend.Run returns.
type Result struct {
	Value string `json:"result"`
}

// ExtractResult returns the agent's final message from Backend.Run output,
// or "" if raw is not valid JSON.
func ExtractResult(raw []byte) string {
	var r Result
	if err := json.Unmarshal(raw, &r); err != nil {
		return ""
	}
	return r.Value
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
