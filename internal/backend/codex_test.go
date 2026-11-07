package backend

import (
	"context"
	"strings"
	"testing"

	"github.com/changkun/ralph/internal/prompt"
)

// codexWrites is a fake codex body that writes "output" to the -o file.
const codexWrites = `while [ $# -gt 0 ]; do case "$1" in -o) echo output > "$2"; shift 2;; *) shift;; esac; done`

// codexArgs replaces the value of -o, a random temporary file, in args.
func codexArgs(args []string) []string {
	for i := range args {
		if args[i] == "-o" && i+1 < len(args) {
			args[i+1] = "<out>"
		}
	}
	return args
}

func TestCodexRun(t *testing.T) {
	tests := []struct {
		role Role
		p    prompt.Prompt
		want []string
	}{
		{Strategist, prompt.Prompt{User: "task"},
			[]string{"exec", "--sandbox", "read-only", "--ephemeral", "-C", "/project", "-o", "<out>", "task"}},
		{Executor, prompt.Prompt{System: "sys", User: "task"},
			[]string{"exec", "--sandbox", "workspace-write", "-C", "/project", "-o", "<out>", "sys\n\ntask"}},
		{Archivist, prompt.Prompt{User: "task"},
			[]string{"exec", "--sandbox", "workspace-write", "-C", "/project", "-o", "<out>", "task"}},
	}
	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			args := fakeCLI(t, "codex", codexWrites)
			raw, err := (&Codex{}).Run(context.Background(), tt.role, "/project", tt.p)
			if err != nil {
				t.Fatal(err)
			}
			if r := ExtractResult(raw); r != "output\n" {
				t.Errorf("result = %q, want %q", r, "output\n")
			}
			assertArgs(t, codexArgs(args()), tt.want)
		})
	}
}

func TestCodexRunNoMessage(t *testing.T) {
	fakeCLI(t, "codex", `exit 0`)
	raw, err := (&Codex{}).Run(context.Background(), Executor, t.TempDir(), prompt.Prompt{User: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if r := ExtractResult(raw); r != "" {
		t.Errorf("result = %q, want empty", r)
	}
}

func TestCodexRunFailure(t *testing.T) {
	fakeCLI(t, "codex", `echo 'error: unexpected argument' >&2; exit 2`)
	_, err := (&Codex{}).Run(context.Background(), Executor, t.TempDir(), prompt.Prompt{User: "task"})
	if err == nil || !strings.Contains(err.Error(), "unexpected argument") {
		t.Errorf("err = %v, want it to include stderr", err)
	}
}

func TestCodexRunCancelled(t *testing.T) {
	fakeCLI(t, "codex", codexWrites)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (&Codex{}).Run(ctx, Executor, t.TempDir(), prompt.Prompt{User: "task"}); err == nil {
		t.Error("expected an error from a cancelled run")
	}
}

func TestCodexTempFileError(t *testing.T) {
	t.Setenv("TMPDIR", "/nonexistent_ralph_test_dir")
	if _, err := (&Codex{}).Run(context.Background(), Strategist, "/project", prompt.Prompt{User: "task"}); err == nil {
		t.Error("expected an error")
	}
}
