package backend

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/changkun/ralph/internal/prompt"
	"github.com/changkun/ralph/internal/transcript"
)

func TestClaudeRunStreams(t *testing.T) {
	fakeCLI(t, "claude", replay(t, "claude.jsonl"))
	var rec recorder
	result, err := (&Claude{}).Run(context.Background(), rec.turn(Turn{Role: Executor, Folder: t.TempDir(), Prompt: prompt.Prompt{User: "task"}}))
	if err != nil {
		t.Fatal(err)
	}
	if result != "done" {
		t.Errorf("result = %q, want %q", result, "done")
	}
	if n := strings.Count(rec.lines.String(), "\n"); n != 5 {
		t.Errorf("transcript has %d lines, want 5", n)
	}
	want := []transcript.Kind{transcript.Info, transcript.ToolCall, transcript.ToolResult, transcript.Message, transcript.Result}
	if !slices.Equal(rec.kinds(), want) {
		t.Errorf("events = %v, want %v", rec.kinds(), want)
	}
}

func TestClaudeRunsInFolder(t *testing.T) {
	fakeCLI(t, "claude", `printf '{"type":"result","result":"%s"}\n' "$(pwd)"`)
	folder := t.TempDir()
	result, err := (&Claude{}).Run(context.Background(), Turn{Role: Executor, Folder: folder, Prompt: prompt.Prompt{User: "task"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := evalSymlinks(t, result), evalSymlinks(t, folder); got != want {
		t.Errorf("ran in %s, want %s", got, want)
	}
}

func TestClaudeArgs(t *testing.T) {
	stream := []string{"-p", "--output-format", "stream-json", "--verbose"}
	readOnlyTools := []string{"--tools", "Read,Glob,Grep", "--strict-mcp-config"}
	skip := []string{"--dangerously-skip-permissions"}
	join := func(parts ...[]string) []string { return slices.Concat(parts...) }
	tests := []struct {
		name     string
		isolated bool
		turn     Turn
		want     []string
	}{
		{"executor", false, Turn{Role: Executor, Model: "opus", Prompt: prompt.Prompt{System: "sys", User: "task"}},
			join(stream, skip, []string{"--model", "opus", "--append-system-prompt", "sys", "task"})},
		{"archivist without system prompt", false, Turn{Role: Archivist, Prompt: prompt.Prompt{User: "task"}},
			join(stream, skip, []string{"task"})},
		{"strategist", false, Turn{Role: Strategist, Prompt: prompt.Prompt{User: "task"}},
			join(stream, readOnlyTools, []string{"task"})},
		{"strategist with system prompt", false, Turn{Role: Strategist, Prompt: prompt.Prompt{System: "sys", User: "task"}},
			join(stream, readOnlyTools, []string{"--append-system-prompt", "sys", "task"})},
		{"isolated strategist", true, Turn{Role: Strategist, Prompt: prompt.Prompt{User: "task"}},
			join(stream, skip, []string{"task"})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := fakeCLI(t, "claude", `echo '{"type":"result","result":"ok"}'`)
			c := &Claude{}
			env := &isolatedEnv{}
			if tt.isolated {
				c.Env = env
			}
			tt.turn.Folder = t.TempDir()
			if _, err := c.Run(context.Background(), tt.turn); err != nil {
				t.Fatal(err)
			}
			assertArgs(t, args(), tt.want)
			if tt.isolated && !slices.Equal(env.readOnly, []bool{tt.turn.Role.ReadOnly()}) {
				t.Errorf("readOnly passed to Env = %v", env.readOnly)
			}
		})
	}
}

func TestClaudeRunFailure(t *testing.T) {
	tests := []struct {
		name, body, want string
	}{
		{"reported failure", `echo '{"type":"result","is_error":true,"result":"Not logged in"}'; exit 1`, "Not logged in"},
		{"reported failure with exit 0", `echo '{"type":"result","is_error":true,"result":"Not logged in"}'`, "Not logged in"},
		{"stderr", `echo 'command not understood' >&2; exit 2`, "command not understood"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeCLI(t, "claude", tt.body)
			_, err := (&Claude{}).Run(context.Background(), Turn{Role: Executor, Folder: t.TempDir(), Prompt: prompt.Prompt{User: "task"}})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to include %q", err, tt.want)
			}
		})
	}
}

func TestClaudeRunSubprocessHoldsOutput(t *testing.T) {
	// The CLI succeeds but leaves a background process holding its stdout.
	fakeCLI(t, "claude", `sleep 10 & echo '{"type":"result","result":"ok"}'`)
	start := time.Now()
	result, err := (&Claude{}).Run(context.Background(), Turn{Role: Executor, Folder: t.TempDir(), Prompt: prompt.Prompt{User: "task"}})
	if err != nil {
		t.Fatal(err)
	}
	if result != "ok" {
		t.Errorf("result = %q, want %q", result, "ok")
	}
	if d := time.Since(start); d > waitDelay+5*time.Second {
		t.Errorf("Run took %v", d)
	}
}

func TestClaudeRunCancelled(t *testing.T) {
	// The CLI's child keeps running, and holding stdout, after the CLI is killed.
	fakeCLI(t, "claude", `sleep 10`)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := (&Claude{}).Run(ctx, Turn{Role: Executor, Folder: t.TempDir(), Prompt: prompt.Prompt{User: "task"}}); err == nil {
		t.Error("expected an error from a cancelled run")
	}
	if d := time.Since(start); d > waitDelay+5*time.Second {
		t.Errorf("Run took %v after cancellation", d)
	}
}

// evalSymlinks resolves path so that paths under /tmp and /private/tmp on
// macOS compare equal.
func evalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}
