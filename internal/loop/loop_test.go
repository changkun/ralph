package loop

import (
	"bytes"
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
	"github.com/changkun/ralph/internal/transcript"
)

type reply func(ctx context.Context) (string, error)

func say(s string) reply { return func(context.Context) (string, error) { return s, nil } }

// fakeBackend answers each role with replies[role], or with "<role> done" if
// there is none, and records the turns it ran. Each turn streams one
// transcript line and one message event.
type fakeBackend struct {
	replies map[backend.Role]reply
	turns   []backend.Turn
}

func (f *fakeBackend) Run(ctx context.Context, t backend.Turn) (string, error) {
	f.turns = append(f.turns, t)
	r, ok := f.replies[t.Role]
	if !ok {
		r = say(string(t.Role) + " done")
	}
	if t.Transcript != nil {
		t.Transcript.Write([]byte(`{"type":"fake"}` + "\n"))
	}
	if t.OnEvent != nil {
		t.OnEvent(transcript.Event{Kind: transcript.Message, Text: "working as " + string(t.Role)})
	}
	return r(ctx)
}

func (f *fakeBackend) roles() []backend.Role {
	var roles []backend.Role
	for _, t := range f.turns {
		roles = append(roles, t.Role)
	}
	return roles
}

func (f *fakeBackend) turn(role backend.Role) backend.Turn {
	for _, t := range f.turns {
		if t.Role == role {
			return t
		}
	}
	return backend.Turn{}
}

// config returns a loop configuration that runs every role of p with be,
// in and logging to dir, for maxRounds rounds.
func config(be backend.Backend, p Pattern, dir string, maxRounds int) Config {
	agents := map[backend.Role]Agent{}
	for _, role := range p.Roles() {
		agents[role] = Agent{Backend: be}
	}
	return Config{
		Pattern:   p,
		Agents:    agents,
		Folder:    dir,
		LogDir:    dir,
		MaxRounds: maxRounds,
		Output:    &bytes.Buffer{},
	}
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

// roundLogs returns the names of both logs of each role in round 1.
func roundLogs(roles ...backend.Role) []string {
	var names []string
	for _, role := range roles {
		names = append(names, "round-001-"+string(role)+".json", "round-001-"+string(role)+".jsonl")
	}
	slices.Sort(names)
	return names
}

func assertEqual[T comparable](t *testing.T, what string, got, want []T) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s:\n got %v\nwant %v", what, got, want)
	}
}

func TestRunPatterns(t *testing.T) {
	for _, p := range Patterns {
		t.Run(string(p), func(t *testing.T) {
			dir := t.TempDir()
			be := &fakeBackend{}
			if err := Run(context.Background(), config(be, p, dir, 1), 0); err != nil {
				t.Fatal(err)
			}
			assertEqual(t, "roles run", be.roles(), p.Roles())
			assertEqual(t, "logs", logs(t, dir), roundLogs(p.Roles()...))
		})
	}
}

func TestRunSavesLogs(t *testing.T) {
	dir := t.TempDir()
	if err := Run(context.Background(), config(&fakeBackend{}, Standalone, dir, 1), 0); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "round-001-standalone.json"))
	if err != nil {
		t.Fatal(err)
	}
	var result struct{ Result string }
	if err := json.Unmarshal(data, &result); err != nil || result.Result != "standalone done" {
		t.Errorf("result log = %s (%v)", data, err)
	}
	data, err = os.ReadFile(filepath.Join(dir, "round-001-standalone.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), `{"type":"fake"}`+"\n"; got != want {
		t.Errorf("transcript = %q, want %q", got, want)
	}
}

func TestRunNumbersRoundsAfterFinished(t *testing.T) {
	dir := t.TempDir()
	if err := Run(context.Background(), config(&fakeBackend{}, Standalone, dir, 4), 2); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "logs", logs(t, dir), []string{
		"round-003-standalone.json", "round-003-standalone.jsonl",
		"round-004-standalone.json", "round-004-standalone.jsonl",
	})
}

func TestRunPassesRoundToLaterRoles(t *testing.T) {
	dir := t.TempDir()
	be := &fakeBackend{}
	cfg := config(be, ThinkActEvaluatorArchivist, dir, 1)
	cfg.AgentFolder = "/work"
	if err := Run(context.Background(), cfg, 0); err != nil {
		t.Fatal(err)
	}
	for _, turn := range be.turns {
		if turn.Folder != "/work" {
			t.Errorf("%s ran in %q, want the agent folder", turn.Role, turn.Folder)
		}
	}
	archivist := be.turn(backend.Archivist).Prompt.User
	for _, want := range []string{"/work", "strategist done", "executor done", "evaluator done", "AGENTS.md"} {
		if !strings.Contains(archivist, want) {
			t.Errorf("archivist prompt does not contain %q:\n%s", want, archivist)
		}
	}
}

func TestRunUsesEachRolesAgent(t *testing.T) {
	dir := t.TempDir()
	thinker, doer := &fakeBackend{}, &fakeBackend{}
	tmpl, err := prompt.Parse(prompt.Text{System: "custom system", Prompt: "work on {{.Objective}}"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config(nil, ThinkAct, dir, 1)
	cfg.Agents = map[backend.Role]Agent{
		backend.Strategist: {Backend: thinker, Model: "big"},
		backend.Executor:   {Backend: doer, Model: "small", Template: tmpl},
	}
	if err := Run(context.Background(), cfg, 0); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "strategist backend ran", thinker.roles(), []backend.Role{backend.Strategist})
	assertEqual(t, "executor backend ran", doer.roles(), []backend.Role{backend.Executor})
	if m := thinker.turns[0].Model; m != "big" {
		t.Errorf("strategist model = %q", m)
	}
	got := doer.turns[0]
	want := prompt.Prompt{System: "custom system", User: "work on strategist done"}
	if got.Model != "small" || got.Prompt != want {
		t.Errorf("executor turn: model %q, prompt %+v; want small, %+v", got.Model, got.Prompt, want)
	}
}

func TestRunRequiresAnAgentPerRole(t *testing.T) {
	cfg := config(&fakeBackend{}, ThinkAct, t.TempDir(), 1)
	delete(cfg.Agents, backend.Executor)
	err := Run(context.Background(), cfg, 0)
	if err == nil || !strings.Contains(err.Error(), "executor") {
		t.Errorf("err = %v, want one naming the executor", err)
	}
}

func TestRunRefusesThinkers(t *testing.T) {
	be := &fakeBackend{}
	err := Run(context.Background(), config(be, Thinkers, t.TempDir(), 1), 0)
	if err == nil || len(be.turns) != 0 {
		t.Errorf("err = %v, turns = %d; want an error and no turns", err, len(be.turns))
	}
}

func TestRunTemplateError(t *testing.T) {
	// The template renders for an empty round but fails on this objective.
	tmpl, err := prompt.Parse(prompt.Text{Prompt: `{{if .Objective}}{{index .Objective 99}}{{end}}`})
	if err != nil {
		t.Fatal(err)
	}
	be := &fakeBackend{}
	cfg := config(be, ThinkAct, t.TempDir(), 1)
	cfg.Agents[backend.Executor] = Agent{Backend: be, Template: tmpl}
	if err := Run(context.Background(), cfg, 0); err == nil || !strings.HasPrefix(err.Error(), "executor: ") {
		t.Errorf("err = %v, want an executor template error", err)
	}
	assertEqual(t, "roles run", be.roles(), []backend.Role{backend.Strategist})
}

func TestRunSkipsRoundWithoutObjective(t *testing.T) {
	for _, p := range []Pattern{ThinkAct, Standalone} {
		t.Run(string(p), func(t *testing.T) {
			be := &fakeBackend{replies: map[backend.Role]reply{
				backend.Strategist: say(""),
				backend.Standalone: say(""),
			}}
			cfg := config(be, p, t.TempDir(), 2)
			if err := Run(context.Background(), cfg, 0); err != nil {
				t.Fatal(err)
			}
			first := p.Roles()[0]
			assertEqual(t, "roles run", be.roles(), []backend.Role{first, first})
			out := cfg.Output.(*bytes.Buffer).String()
			if !strings.Contains(out, "produced no output") || !strings.Contains(out, "Round 2 skipped") {
				t.Errorf("output does not report the skipped rounds:\n%s", out)
			}
		})
	}
}

func TestRunContinuesAfterEmptyExecutorResult(t *testing.T) {
	be := &fakeBackend{replies: map[backend.Role]reply{backend.Executor: say("")}}
	if err := Run(context.Background(), config(be, ThinkActEvaluator, t.TempDir(), 1), 0); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "roles run", be.roles(), ThinkActEvaluator.Roles())
}

func TestRunStopsOnAgentError(t *testing.T) {
	dir := t.TempDir()
	be := &fakeBackend{replies: map[backend.Role]reply{
		backend.Executor: func(context.Context) (string, error) { return "", errors.New("not logged in") },
	}}
	err := Run(context.Background(), config(be, ThinkActEvaluator, dir, 0), 0)
	if err == nil || err.Error() != "executor: not logged in" {
		t.Errorf("err = %v, want %q", err, "executor: not logged in")
	}
	assertEqual(t, "roles run", be.roles(), []backend.Role{"strategist", "executor"})
	// The failed turn keeps its transcript but has no result.
	assertEqual(t, "logs", logs(t, dir), []string{
		"round-001-executor.jsonl", "round-001-strategist.json", "round-001-strategist.jsonl",
	})
}

func TestRunStopsOnInterrupt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	be := &fakeBackend{replies: map[backend.Role]reply{
		backend.Executor: func(ctx context.Context) (string, error) { cancel(); return "", ctx.Err() },
	}}
	if err := Run(ctx, config(be, ThinkActEvaluator, t.TempDir(), 0), 0); err != nil {
		t.Errorf("err = %v, want nil after an interrupt", err)
	}
	assertEqual(t, "roles run", be.roles(), []backend.Role{"strategist", "executor"})
}

func TestRunRecreatesLogDir(t *testing.T) {
	dir := t.TempDir()
	cfg := config(&fakeBackend{}, Standalone, dir, 1)
	cfg.LogDir = filepath.Join(dir, ".ralph")
	if err := Run(context.Background(), cfg, 0); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "logs", logs(t, cfg.LogDir), roundLogs(backend.Standalone))
}

func TestRunWithoutLogDir(t *testing.T) {
	cfg := config(&fakeBackend{}, Standalone, t.TempDir(), 1)
	cfg.LogDir = "/dev/null/logs"
	if err := Run(context.Background(), cfg, 0); err != nil {
		t.Fatal(err)
	}
	out := cfg.Output.(*bytes.Buffer).String()
	for _, want := range []string{"Could not save the standalone transcript", "Could not save the standalone log"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not contain %q:\n%s", want, out)
		}
	}
}

func TestRunCallsOnStep(t *testing.T) {
	type step struct {
		round int
		role  backend.Role
	}
	var steps []step
	cfg := config(&fakeBackend{}, ThinkAct, t.TempDir(), 2)
	cfg.OnStep = func(round int, role backend.Role) { steps = append(steps, step{round, role}) }
	if err := Run(context.Background(), cfg, 0); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "steps", steps, []step{
		{1, backend.Strategist}, {1, backend.Executor},
		{2, backend.Strategist}, {2, backend.Executor},
	})
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
	cfg := config(be, ThinkAct, dir, 1)
	cfg.LogDir = filepath.Join(dir, ".ralph")
	if err := Run(context.Background(), cfg, 0); err != nil {
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

func TestPrintEvent(t *testing.T) {
	tests := []struct {
		event transcript.Event
		want  string
	}{
		{transcript.Event{Kind: transcript.Info, Title: "Claude Code 2.1 · opus"}, "  · Claude Code 2.1 · opus\n"},
		{transcript.Event{Kind: transcript.Message, Text: " Done.\n"}, "\nDone.\n\n"},
		{transcript.Event{Kind: transcript.Thinking, Text: "hmm"}, ""},
		{transcript.Event{Kind: transcript.ToolCall, Title: "Read main.go"}, "  ▸ Read main.go\n"},
		{transcript.Event{Kind: transcript.FileChange, Title: "update main.go"}, "  ▸ update main.go\n"},
		{transcript.Event{Kind: transcript.Command, Title: "go test ./..."}, "  $ go test ./...\n"},
		{transcript.Event{Kind: transcript.ToolResult, Text: "ok"}, ""},
		{transcript.Event{Kind: transcript.ToolResult, Text: "no such file\nmore", Failed: true}, "    ✗ no such file\n"},
		{transcript.Event{Kind: transcript.Error, Text: "rate limited\nretrying"}, "  [!] rate limited\n"},
		{transcript.Event{Kind: transcript.Result, Title: "Finished in 3s"}, "  ✓ Finished in 3s\n"},
		{transcript.Event{Kind: transcript.Result, Title: "Failed", Failed: true}, "  ✗ Failed\n"},
	}
	for _, tt := range tests {
		var b strings.Builder
		printEvent(&b, tt.event)
		if b.String() != tt.want {
			t.Errorf("printEvent(%+v) = %q, want %q", tt.event, b.String(), tt.want)
		}
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
		{"**Objective: Add `hello.py`.**", "ralph: Objective: Add `hello.py`."},
		{"## Add a CLI", "ralph: Add a CLI"},
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
