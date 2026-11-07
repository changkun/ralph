// Package backend runs agent turns through an external coding-agent CLI,
// on this machine or in an isolated environment such as a container.
package backend

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/changkun/ralph/internal/prompt"
	"github.com/changkun/ralph/internal/transcript"
)

// Role identifies the agent role a turn is run for. Its value also names the
// round log files (round-NNN-<role>.json and .jsonl) Ralph writes for it.
type Role string

// Agent roles. Standalone is the only role in the standalone pattern, and
// Thinker the only one in the thinkers pattern; the others make up the
// strategist pipeline.
const (
	Strategist Role = "strategist"
	Executor   Role = "executor"
	Evaluator  Role = "evaluator"
	Archivist  Role = "archivist"
	Standalone Role = "standalone"
	Thinker    Role = "thinker"
)

// ReadOnly reports whether role only reasons about the project and so must
// not be able to change it. Only the strategist is read-only.
func (r Role) ReadOnly() bool { return r == Strategist }

// Turn is one agent turn.
type Turn struct {
	Role   Role
	Folder string // the project folder, as the agent sees it
	Model  string // the model to use; empty for the CLI's default
	Prompt prompt.Prompt

	// Transcript, if not nil, receives each line of the CLI's JSONL event
	// stream as it arrives.
	Transcript io.Writer
	// OnEvent, if not nil, is called with each transcript event as it
	// arrives.
	OnEvent func(transcript.Event)
}

// Backend runs agent turns.
type Backend interface {
	// Run runs t and returns the agent's final answer. If the CLI fails, or
	// reports that the turn failed, the error says why.
	Run(ctx context.Context, t Turn) (string, error)
}

// Env is where agent CLIs run.
type Env interface {
	// Command returns a command that runs the CLI name with args in dir. If
	// readOnly is set, the agent must not be able to change dir.
	Command(ctx context.Context, readOnly bool, dir, name string, args ...string) *exec.Cmd

	// Isolated reports whether the environment itself confines agents and
	// enforces readOnly, so the CLIs need no sandbox of their own.
	Isolated() bool
}

// Host runs agent CLIs directly on this machine, where only the CLIs' own
// permission controls restrict them.
type Host struct{}

// Command implements Env.
func (Host) Command(ctx context.Context, _ bool, dir, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	return cmd
}

// Isolated implements Env.
func (Host) Isolated() bool { return false }

// New returns the backend called name ("claude" or "codex") that runs
// agents in env, or nil if there is none. A nil env means Host.
func New(name string, env Env) Backend {
	switch name {
	case "claude":
		return &Claude{Env: env}
	case "codex":
		return &Codex{Env: env}
	}
	return nil
}

// Names lists the backend names New accepts.
var Names = []string{"claude", "codex"}

func envOrHost(env Env) Env {
	if env == nil {
		return Host{}
	}
	return env
}

// waitDelay bounds how long a turn waits, once the CLI has exited or been
// killed, for subprocesses started by the agent to release the CLI's output.
const waitDelay = 2 * time.Second

// stream runs cmd, a CLI that prints a JSONL transcript, and passes each
// line to t's Transcript and OnEvent as it arrives. It returns the agent's
// final answer.
func stream(cli string, cmd *exec.Cmd, t Turn) (string, error) {
	var turn transcript.Turn
	lines := &lineWriter{fn: func(line []byte) {
		if t.Transcript != nil {
			t.Transcript.Write(append(line, '\n'))
		}
		for _, e := range turn.Add(line) {
			if t.OnEvent != nil {
				t.OnEvent(e)
			}
		}
	}}
	stderr := &tailBuffer{max: 8 << 10}
	cmd.Stdout = lines
	cmd.Stderr = stderr
	cmd.WaitDelay = waitDelay
	err := cmd.Run()
	lines.flush()
	if errors.Is(err, exec.ErrWaitDelay) {
		// The CLI succeeded, but a subprocess it started (a server, say)
		// still holds its output open. The CLI's own output is complete.
		err = nil
	}
	if err != nil {
		return "", runError(cli, err, turn.Failure(), stderr.Bytes())
	}
	if failure := turn.Failure(); failure != "" {
		return "", fmt.Errorf("%s: %s", cli, failure)
	}
	return turn.Result(), nil
}

// runError wraps the error from a failed CLI run with the most specific
// diagnostic available: the failure the CLI reported in its transcript, or
// else the last line it wrote to stderr.
func runError(cli string, err error, failure string, stderr []byte) error {
	if failure == "" {
		failure = lastLine(stderr)
	}
	if failure == "" {
		return fmt.Errorf("%s: %w", cli, err)
	}
	return fmt.Errorf("%s: %w: %s", cli, err, failure)
}

func lastLine(b []byte) string {
	b = bytes.TrimSpace(b)
	if i := bytes.LastIndexByte(b, '\n'); i >= 0 {
		b = bytes.TrimSpace(b[i+1:])
	}
	return string(b)
}

// lineWriter is an io.Writer that calls fn with each complete line written
// to it, without the newline.
type lineWriter struct {
	fn  func(line []byte)
	buf []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		if line := bytes.TrimRight(w.buf[:i], "\r"); len(line) > 0 {
			w.fn(bytes.Clone(line))
		}
		w.buf = w.buf[i+1:]
	}
}

// flush passes on a final line that has no newline.
func (w *lineWriter) flush() {
	if len(bytes.TrimSpace(w.buf)) > 0 {
		w.fn(bytes.Clone(w.buf))
	}
	w.buf = nil
}

// tailBuffer is an io.Writer that keeps the last max bytes written to it.
type tailBuffer struct {
	max int
	buf []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.max {
		b.buf = b.buf[len(b.buf)-b.max:]
	}
	return len(p), nil
}

func (b *tailBuffer) Bytes() []byte { return b.buf }
