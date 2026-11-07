package loop

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/changkun/ralph/internal/backend"
	"github.com/changkun/ralph/internal/git"
	"github.com/changkun/ralph/internal/prompt"
)

type reply func(ctx context.Context) (string, error)

func say(s string) reply { return func(context.Context) (string, error) { return s, nil } }

// fakeBackend answers each role with replies[role], or with "<role> done" if
// there is none, and records the roles it ran and the prompts it received.
type fakeBackend struct {
	replies map[backend.Role]reply
	calls   []backend.Role
	prompts map[backend.Role]prompt.Prompt
}

func (f *fakeBackend) Run(ctx context.Context, role backend.Role, _ string, p prompt.Prompt) ([]byte, error) {
	f.calls = append(f.calls, role)
	if f.prompts == nil {
		f.prompts = map[backend.Role]prompt.Prompt{}
	}
	f.prompts[role] = p
	r, ok := f.replies[role]
	if !ok {
		r = say(string(role) + " done")
	}
	result, err := r(ctx)
	if err != nil {
		return nil, err
	}
	return json.Marshal(backend.Result{Value: result})
}

// logs returns the names of the round logs in dir.
func logs(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func assertEqual[T comparable](t *testing.T, what string, got, want []T) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s:\n got %v\nwant %v", what, got, want)
	}
}

func TestRunPatterns(t *testing.T) {
	tests := []struct {
		pattern Pattern
		roles   []backend.Role
	}{
		{Standalone, []backend.Role{"standalone"}},
		{ThinkAct, []backend.Role{"strategist", "executor"}},
		{ThinkActEvaluator, []backend.Role{"strategist", "executor", "evaluator"}},
		{ThinkActEvaluatorArchivist, []backend.Role{"strategist", "executor", "evaluator", "archivist"}},
	}
	for _, tt := range tests {
		t.Run(string(tt.pattern), func(t *testing.T) {
			dir := t.TempDir()
			be := &fakeBackend{}
			if err := Run(context.Background(), be, tt.pattern, dir, dir, 0, 1); err != nil {
				t.Fatal(err)
			}
			assertEqual(t, "roles run", be.calls, tt.roles)
			var want []string
			for _, role := range tt.roles {
				want = append(want, "round-001-"+string(role)+".json")
			}
			slices.Sort(want)
			assertEqual(t, "logs", logs(t, dir), want)
		})
	}
}

func TestRunNumbersRoundsAfterFinished(t *testing.T) {
	dir := t.TempDir()
	if err := Run(context.Background(), &fakeBackend{}, ThinkAct, dir, dir, 2, 4); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "logs", logs(t, dir), []string{
		"round-003-executor.json", "round-003-strategist.json",
		"round-004-executor.json", "round-004-strategist.json",
	})
}

func TestRunPassesRoundToLaterRoles(t *testing.T) {
	dir := t.TempDir()
	be := &fakeBackend{}
	if err := Run(context.Background(), be, ThinkActEvaluatorArchivist, dir, dir, 0, 1); err != nil {
		t.Fatal(err)
	}
	archivist := be.prompts[backend.Archivist].User
	for _, want := range []string{dir, "strategist done", "executor done", "evaluator done", "AGENTS.md"} {
		if !strings.Contains(archivist, want) {
			t.Errorf("archivist prompt does not contain %q:\n%s", want, archivist)
		}
	}
}

func TestRunSkipsRoundWithoutObjective(t *testing.T) {
	for _, p := range []Pattern{ThinkAct, Standalone} {
		t.Run(string(p), func(t *testing.T) {
			dir := t.TempDir()
			be := &fakeBackend{replies: map[backend.Role]reply{
				backend.Strategist: say(""),
				backend.Standalone: say(""),
			}}
			if err := Run(context.Background(), be, p, dir, dir, 0, 2); err != nil {
				t.Fatal(err)
			}
			first := p.roles()[0]
			assertEqual(t, "roles run", be.calls, []backend.Role{first, first})
		})
	}
}

func TestRunContinuesAfterEmptyExecutorResult(t *testing.T) {
	dir := t.TempDir()
	be := &fakeBackend{replies: map[backend.Role]reply{backend.Executor: say("")}}
	if err := Run(context.Background(), be, ThinkActEvaluator, dir, dir, 0, 1); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "roles run", be.calls, []backend.Role{"strategist", "executor", "evaluator"})
}

func TestRunStopsOnAgentError(t *testing.T) {
	dir := t.TempDir()
	be := &fakeBackend{replies: map[backend.Role]reply{
		backend.Executor: func(context.Context) (string, error) { return "", errors.New("not logged in") },
	}}
	err := Run(context.Background(), be, ThinkActEvaluator, dir, dir, 0, 0)
	if err == nil || err.Error() != "executor: not logged in" {
		t.Errorf("err = %v, want %q", err, "executor: not logged in")
	}
	assertEqual(t, "roles run", be.calls, []backend.Role{"strategist", "executor"})
	assertEqual(t, "logs", logs(t, dir), []string{"round-001-strategist.json"})
}

func TestRunStopsOnInterrupt(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	be := &fakeBackend{replies: map[backend.Role]reply{
		backend.Executor: func(ctx context.Context) (string, error) { cancel(); return "", ctx.Err() },
	}}
	if err := Run(ctx, be, ThinkActEvaluator, dir, dir, 0, 0); err != nil {
		t.Errorf("err = %v, want nil after an interrupt", err)
	}
	assertEqual(t, "roles run", be.calls, []backend.Role{"strategist", "executor"})
}

func TestRunRecreatesLogDir(t *testing.T) {
	dir := t.TempDir()
	ralphDir := filepath.Join(dir, ".ralph")
	if err := Run(context.Background(), &fakeBackend{}, Standalone, dir, ralphDir, 0, 1); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "logs", logs(t, ralphDir), []string{"round-001-standalone.json"})
}

func TestRunCommitsEachRound(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "T"},
		{"config", "commit.gpgsign", "false"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	be := &fakeBackend{replies: map[backend.Role]reply{
		backend.Executor: func(context.Context) (string, error) {
			return "wrote f.txt", os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644)
		},
	}}
	if err := Run(context.Background(), be, ThinkAct, dir, filepath.Join(dir, ".ralph"), 0, 1); err != nil {
		t.Fatal(err)
	}
	if git.HasChanges(dir) {
		t.Error("changes left uncommitted")
	}
	out, err := exec.Command("git", "-C", dir, "log", "-1", "--format=%s").Output()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(out)), "ralph: strategist done"; got != want {
		t.Errorf("commit subject = %q, want %q", got, want)
	}
}

func TestCommitMessage(t *testing.T) {
	long := strings.Repeat("a", 70)
	wide := strings.Repeat("é", 70)
	tests := []struct {
		summary, want string
	}{
		{"", "ralph: update project"},
		{"  \n  ", "ralph: update project"},
		{"- : -", "ralph: update project"},
		{"Add a CLI", "ralph: Add a CLI"},
		{"- Add   a\tCLI:\nwith details", "ralph: Add a CLI"},
		{long, "ralph: " + long[:60]},
		{wide, "ralph: " + strings.Repeat("é", 60)},
	}
	for _, tt := range tests {
		got := commitMessage(tt.summary)
		if got != tt.want {
			t.Errorf("commitMessage(%q) = %q, want %q", tt.summary, got, tt.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("commitMessage(%q) is not valid UTF-8", tt.summary)
		}
	}
}
