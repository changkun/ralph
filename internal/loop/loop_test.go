package loop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/changkun/ralph/internal/backend"
	"github.com/changkun/ralph/internal/git"
	"github.com/changkun/ralph/internal/prompt"
)

type mockBE struct {
	thinker, worker, archivist func() (string, error)
}

func (m *mockBE) RunThinker(_ context.Context, _ string, _ prompt.Prompt) ([]byte, error) {
	r, err := m.thinker()
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(backend.Result{Value: r})
	return b, nil
}

func (m *mockBE) RunWorker(_ context.Context, _ string, _ prompt.Prompt) ([]byte, error) {
	r, err := m.worker()
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(backend.Result{Value: r})
	return b, nil
}

func (m *mockBE) RunArchivist(_ context.Context, _ string, _ prompt.Prompt) ([]byte, error) {
	r, err := m.archivist()
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(backend.Result{Value: r})
	return b, nil
}

func ok(s string) func() (string, error) { return func() (string, error) { return s, nil } }
func fail() func() (string, error)       { return func() (string, error) { return "", errors.New("err") } }

func setup(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	rd := filepath.Join(dir, ".ralph")
	os.MkdirAll(rd, 0o755)
	return dir, rd
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	for _, a := range [][]string{
		{"init"}, {"config", "user.email", "t@t"}, {"config", "user.name", "T"},
	} {
		exec.Command("git", append([]string{"-C", dir}, a...)...).Run()
	}
	os.WriteFile(filepath.Join(dir, ".gitkeep"), nil, 0o644)
	exec.Command("git", "-C", dir, "add", ".").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "init").Run()
}

func TestNormal(t *testing.T) {
	dir, rd := setup(t)
	r := 0
	RunThinkAct(context.Background(), &mockBE{ok("idea"), ok("done"), nil}, dir, rd, &r, 1)
	if r != 1 {
		t.Errorf("round=%d", r)
	}
}

func TestEmptyWorker(t *testing.T) {
	dir, rd := setup(t)
	r := 0
	RunThinkAct(context.Background(), &mockBE{ok("idea"), ok(""), nil}, dir, rd, &r, 1)
}

func TestEmptyThinker(t *testing.T) {
	dir, rd := setup(t)
	r := 0
	RunThinkAct(context.Background(), &mockBE{ok(""), ok(""), nil}, dir, rd, &r, 1)
}

func TestThinkerCancel(t *testing.T) {
	dir, rd := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := 0
	RunThinkAct(ctx, &mockBE{fail(), ok(""), nil}, dir, rd, &r, 0)
}

func TestWorkerCancel(t *testing.T) {
	dir, rd := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	r := 0
	be := &mockBE{ok("idea"), func() (string, error) { cancel(); return "", errors.New("e") }, nil}
	RunThinkAct(ctx, be, dir, rd, &r, 0)
}

func TestGitCommit(t *testing.T) {
	dir, rd := setup(t)
	initRepo(t, dir)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644)
	r := 0
	RunThinkAct(context.Background(), &mockBE{ok("idea"), ok("done"), nil}, dir, rd, &r, 1)
	if git.HasChanges(dir) {
		t.Fatal("expected changes to be committed")
	}
}

func TestThinkActEvaluator(t *testing.T) {
	dir, rd := setup(t)
	r := 0
	RunThinkActEvaluator(context.Background(), &mockBE{ok("idea"), ok("done"), nil}, dir, rd, &r, 1)
	if _, err := os.Stat(filepath.Join(rd, "round-001-evaluator.json")); err != nil {
		t.Fatal(err)
	}
}

func TestThinkActEvaluatorArchivist(t *testing.T) {
	dir, rd := setup(t)
	r := 0
	RunThinkActEvaluatorArchivist(context.Background(), &mockBE{ok("idea"), ok("done"), ok("documented")}, dir, rd, "CLAUDE.md", &r, 1)
	if _, err := os.Stat(filepath.Join(rd, "round-001-archivist.json")); err != nil {
		t.Fatal(err)
	}
}

func TestStandaloneNormal(t *testing.T) {
	dir, rd := setup(t)
	r := 0
	RunStandalone(context.Background(), &mockBE{nil, ok("built it"), nil}, dir, rd, &r, 1)
	if r != 1 {
		t.Errorf("round=%d", r)
	}
}

func TestStandaloneEmpty(t *testing.T) {
	dir, rd := setup(t)
	r := 0
	RunStandalone(context.Background(), &mockBE{nil, ok(""), nil}, dir, rd, &r, 1)
}

func TestStandaloneCancel(t *testing.T) {
	dir, rd := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := 0
	RunStandalone(ctx, &mockBE{nil, fail(), nil}, dir, rd, &r, 0)
}

func TestResumeRoundStandalone(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "round-001-standalone.json"), []byte(`{}`), 0o644)
	os.WriteFile(filepath.Join(dir, "round-002-standalone.json"), []byte(`{}`), 0o644)
	if r := ResumeRound(dir, "standalone"); r != 2 {
		t.Errorf("standalone rounds: got %d, want 2", r)
	}
}

func TestResumeRoundBuilderCompatibility(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "round-001-builder.json"), []byte(`{}`), 0o644)
	os.WriteFile(filepath.Join(dir, "round-002-builder.json"), []byte(`{}`), 0o644)
	if r := ResumeRound(dir, "standalone"); r != 2 {
		t.Errorf("builder compatibility rounds: got %d, want 2", r)
	}
}

func TestResumeRoundThinkAct(t *testing.T) {
	dir := t.TempDir()
	if r := ResumeRound(dir, "think+act"); r != 0 {
		t.Errorf("empty dir: got %d", r)
	}
	for i := 1; i <= 3; i++ {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("round-%03d-strategist.json", i)), []byte(`{}`), 0o644)
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("round-%03d-executor.json", i)), []byte(`{}`), 0o644)
	}
	if r := ResumeRound(dir, "think+act"); r != 3 {
		t.Errorf("3 rounds: got %d", r)
	}
	if r := ResumeRound("/nonexistent_ralph_test", "think+act"); r != 0 {
		t.Errorf("bad dir: got %d", r)
	}
}

func TestResumeRoundWorkerCompatibility(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "round-001-worker.json"), []byte(`{}`), 0o644)
	os.WriteFile(filepath.Join(dir, "round-002-worker.json"), []byte(`{}`), 0o644)
	if r := ResumeRound(dir, "think+act"); r != 2 {
		t.Errorf("worker compatibility rounds: got %d, want 2", r)
	}
}

func TestResumeRoundThinkActEvaluator(t *testing.T) {
	dir := t.TempDir()
	for i := 1; i <= 2; i++ {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("round-%03d-evaluator.json", i)), []byte(`{}`), 0o644)
	}
	if r := ResumeRound(dir, "think+act+evaluator"); r != 2 {
		t.Errorf("evaluator rounds: got %d, want 2", r)
	}
}

func TestResumeRoundThinkActEvaluatorCompatibility(t *testing.T) {
	dir := t.TempDir()
	for i := 1; i <= 2; i++ {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("round-%03d-tester.json", i)), []byte(`{}`), 0o644)
	}
	if r := ResumeRound(dir, "think+act+evaluator"); r != 2 {
		t.Errorf("tester compatibility rounds: got %d, want 2", r)
	}
}

func TestResumeRoundThinkActEvaluatorArchivist(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "round-001-evaluator.json"), []byte(`{}`), 0o644)
	os.WriteFile(filepath.Join(dir, "round-001-archivist.json"), []byte(`{}`), 0o644)
	if r := ResumeRound(dir, "think+act+evaluator+archivist"); r != 1 {
		t.Errorf("archivist rounds: got %d, want 1", r)
	}
}

func TestResumeRoundThinkActEvaluatorArchivistCompatibility(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "round-001-tester.json"), []byte(`{}`), 0o644)
	os.WriteFile(filepath.Join(dir, "round-001-documenter.json"), []byte(`{}`), 0o644)
	if r := ResumeRound(dir, "think+act+evaluator+archivist"); r != 1 {
		t.Errorf("documenter compatibility rounds: got %d, want 1", r)
	}
}
