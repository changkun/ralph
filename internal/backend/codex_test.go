package backend

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/changkun/ralph/internal/prompt"
	"github.com/changkun/ralph/internal/transcript"
)

func TestCodexRunStreams(t *testing.T) {
	fakeCLI(t, "codex", replay(t, "codex.jsonl"))
	var rec recorder
	result, err := (&Codex{}).Run(context.Background(), rec.turn(Turn{Role: Executor, Folder: t.TempDir(), Prompt: prompt.Prompt{User: "task"}}))
	if err != nil {
		t.Fatal(err)
	}
	if result != "done" {
		t.Errorf("result = %q, want the last message %q", result, "done")
	}
	want := []transcript.Kind{transcript.Info, transcript.Command, transcript.Message, transcript.Result}
	if !slices.Equal(rec.kinds(), want) {
		t.Errorf("events = %v, want %v", rec.kinds(), want)
	}
}

func TestCodexArgs(t *testing.T) {
	head := []string{"exec", "--json", "--skip-git-repo-check", "--sandbox"}
	tests := []struct {
		name     string
		isolated bool
		turn     Turn
		want     []string
	}{
		{"strategist", false, Turn{Role: Strategist, Prompt: prompt.Prompt{User: "task"}},
			slices.Concat(head, []string{"read-only", "--ephemeral", "-C", "/project", "task"})},
		{"executor", false, Turn{Role: Executor, Model: "gpt-5", Prompt: prompt.Prompt{System: "sys", User: "task"}},
			slices.Concat(head, []string{"workspace-write", "-m", "gpt-5", "-C", "/project", "sys\n\ntask"})},
		{"isolated executor", true, Turn{Role: Executor, Prompt: prompt.Prompt{User: "task"}},
			slices.Concat(head, []string{"danger-full-access", "-C", "/project", "task"})},
		{"isolated strategist", true, Turn{Role: Strategist, Prompt: prompt.Prompt{User: "task"}},
			slices.Concat(head, []string{"danger-full-access", "--ephemeral", "-C", "/project", "task"})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := fakeCLI(t, "codex", `echo '{"type":"item.completed","item":{"type":"agent_message","text":"ok"}}'`)
			c := &Codex{}
			env := &isolatedEnv{}
			if tt.isolated {
				c.Env = env
			}
			tt.turn.Folder = t.TempDir()
			result, err := c.Run(context.Background(), tt.turn)
			if err != nil || result != "ok" {
				t.Fatalf("Run = %q, %v", result, err)
			}
			got := args()
			if i := slices.Index(got, tt.turn.Folder); i >= 0 {
				got[i] = "/project"
			}
			assertArgs(t, got, tt.want)
			if tt.isolated && !slices.Equal(env.readOnly, []bool{tt.turn.Role.ReadOnly()}) {
				t.Errorf("readOnly passed to Env = %v", env.readOnly)
			}
		})
	}
}

func TestCodexRunFailure(t *testing.T) {
	tests := []struct {
		name, body, want string
	}{
		{"reported failure", replay(t, "codex-failed.jsonl") + "; exit 1", "not supported"},
		{"stderr", `echo 'error: unexpected argument' >&2; exit 2`, "unexpected argument"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeCLI(t, "codex", tt.body)
			_, err := (&Codex{}).Run(context.Background(), Turn{Role: Executor, Folder: t.TempDir(), Prompt: prompt.Prompt{User: "task"}})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to include %q", err, tt.want)
			}
		})
	}
}

func TestCodexRunCancelled(t *testing.T) {
	fakeCLI(t, "codex", `sleep 10`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (&Codex{}).Run(ctx, Turn{Role: Executor, Folder: t.TempDir(), Prompt: prompt.Prompt{User: "task"}}); err == nil {
		t.Error("expected an error from a cancelled run")
	}
}
