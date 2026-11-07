package backend

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/changkun/ralph/internal/prompt"
)

func TestClaudeRun(t *testing.T) {
	// The fake reports the directory it ran in as its result.
	args := fakeCLI(t, "claude", `printf '{"result":"%s"}' "$(pwd)"`)
	folder := t.TempDir()
	raw, err := (&Claude{}).Run(context.Background(), Executor, folder, prompt.Prompt{System: "sys", User: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := evalSymlinks(t, ExtractResult(raw)), evalSymlinks(t, folder); got != want {
		t.Errorf("ran in %s, want %s", got, want)
	}
	assertArgs(t, args(), []string{
		"-p", "--output-format", "json", "--dangerously-skip-permissions",
		"--append-system-prompt", "sys", "task",
	})
}

func TestClaudeRunNoSystemPrompt(t *testing.T) {
	args := fakeCLI(t, "claude", `echo '{"result":"ok"}'`)
	if _, err := (&Claude{}).Run(context.Background(), Archivist, t.TempDir(), prompt.Prompt{User: "task"}); err != nil {
		t.Fatal(err)
	}
	assertArgs(t, args(), []string{"-p", "--output-format", "json", "--dangerously-skip-permissions", "task"})
}

func TestClaudeRunStrategistReadOnly(t *testing.T) {
	tests := []struct {
		p    prompt.Prompt
		want []string
	}{
		{prompt.Prompt{User: "task"}, []string{
			"-p", "--output-format", "json", "--tools", "Read,Glob,Grep", "--strict-mcp-config", "task",
		}},
		{prompt.Prompt{System: "sys", User: "task"}, []string{
			"-p", "--output-format", "json", "--tools", "Read,Glob,Grep", "--strict-mcp-config",
			"--append-system-prompt", "sys", "task",
		}},
	}
	for _, tt := range tests {
		args := fakeCLI(t, "claude", `echo '{"result":"ok"}'`)
		if _, err := (&Claude{}).Run(context.Background(), Strategist, t.TempDir(), tt.p); err != nil {
			t.Fatal(err)
		}
		assertArgs(t, args(), tt.want)
	}
}

func TestClaudeRunFailure(t *testing.T) {
	fakeCLI(t, "claude", `echo '{"result":"Not logged in"}'; exit 1`)
	_, err := (&Claude{}).Run(context.Background(), Executor, t.TempDir(), prompt.Prompt{User: "task"})
	if err == nil || !strings.Contains(err.Error(), "Not logged in") {
		t.Errorf("err = %v, want it to include the reported message", err)
	}
}

func TestClaudeRunFailureStderr(t *testing.T) {
	fakeCLI(t, "claude", `echo 'command not understood' >&2; exit 2`)
	_, err := (&Claude{}).Run(context.Background(), Executor, t.TempDir(), prompt.Prompt{User: "task"})
	if err == nil || !strings.Contains(err.Error(), "command not understood") {
		t.Errorf("err = %v, want it to include stderr", err)
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

func TestClaudeRunSubprocessHoldsOutput(t *testing.T) {
	// The CLI succeeds but leaves a background process holding its stdout.
	fakeCLI(t, "claude", `sleep 10 & echo '{"result":"ok"}'`)
	start := time.Now()
	raw, err := (&Claude{}).Run(context.Background(), Executor, t.TempDir(), prompt.Prompt{User: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if r := ExtractResult(raw); r != "ok" {
		t.Errorf("result = %q, want %q", r, "ok")
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
	if _, err := (&Claude{}).Run(ctx, Executor, t.TempDir(), prompt.Prompt{User: "task"}); err == nil {
		t.Error("expected an error from a cancelled run")
	}
	if d := time.Since(start); d > waitDelay+5*time.Second {
		t.Errorf("Run took %v after cancellation", d)
	}
}
