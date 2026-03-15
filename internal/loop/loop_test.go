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

	"changkun.de/ralph/internal/backend"
	"changkun.de/ralph/internal/prompt"
)

type mockBE struct {
	thinker, worker func() (string, error)
	commit          func() (string, error)
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

func (m *mockBE) RunCommitter(_ context.Context, _ string, _ prompt.Prompt) (string, error) {
	return m.commit()
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
	fakeDir := t.TempDir()
	os.WriteFile(filepath.Join(fakeDir, "gh"), []byte("#!/bin/sh\ntrue"), 0o755)
	t.Setenv("PATH", fakeDir+":"+os.Getenv("PATH"))
}

func TestNormal(t *testing.T) {
	dir, rd := setup(t)
	r := 0
	Run(context.Background(), &mockBE{ok("idea"), ok("done"), nil}, dir, rd, &r, 1)
	if r != 1 {
		t.Errorf("round=%d", r)
	}
}

func TestEmptyWorker(t *testing.T) {
	dir, rd := setup(t)
	r := 0
	Run(context.Background(), &mockBE{ok("idea"), ok(""), nil}, dir, rd, &r, 1)
}

func TestEmptyThinker(t *testing.T) {
	dir, rd := setup(t)
	r := 0
	Run(context.Background(), &mockBE{ok(""), ok(""), nil}, dir, rd, &r, 1)
}

func TestThinkerCancel(t *testing.T) {
	dir, rd := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := 0
	Run(ctx, &mockBE{fail(), ok(""), nil}, dir, rd, &r, 0)
}

func TestWorkerCancel(t *testing.T) {
	dir, rd := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	r := 0
	be := &mockBE{ok("idea"), func() (string, error) { cancel(); return "", errors.New("e") }, nil}
	Run(ctx, be, dir, rd, &r, 0)
}

func TestGitCommit(t *testing.T) {
	dir, rd := setup(t)
	initRepo(t, dir)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644)
	r := 0
	Run(context.Background(), &mockBE{ok("idea"), ok("done"), ok("committed")}, dir, rd, &r, 1)
}

func TestGitCommitEmpty(t *testing.T) {
	dir, rd := setup(t)
	initRepo(t, dir)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644)
	r := 0
	Run(context.Background(), &mockBE{ok("idea"), ok("done"), ok("")}, dir, rd, &r, 1)
}

func TestGitCommitErr(t *testing.T) {
	dir, rd := setup(t)
	initRepo(t, dir)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644)
	r := 0
	Run(context.Background(), &mockBE{ok("idea"), ok("done"), fail()}, dir, rd, &r, 1)
}


func TestResumeRound(t *testing.T) {
	dir := t.TempDir()
	if r := ResumeRound(dir); r != 0 {
		t.Errorf("empty dir: got %d", r)
	}
	for i := 1; i <= 3; i++ {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("round-%03d-thinker.json", i)), []byte(`{}`), 0o644)
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("round-%03d-worker.json", i)), []byte(`{}`), 0o644)
	}
	if r := ResumeRound(dir); r != 3 {
		t.Errorf("3 rounds: got %d", r)
	}
	if r := ResumeRound("/nonexistent_ralph_test"); r != 0 {
		t.Errorf("bad dir: got %d", r)
	}
}

func TestPreviousIdeas(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "round-001-thinker.json"), []byte(`{"result":"idea1"}`), 0o644)
	os.WriteFile(filepath.Join(dir, "round-002-thinker.json"), []byte(`{"result":"idea2"}`), 0o644)
	ideas := PreviousIdeas(dir, 3)
	if len(ideas) != 2 {
		t.Errorf("got %d ideas", len(ideas))
	}
	if ideas := PreviousIdeas(dir, 0); len(ideas) != 0 {
		t.Errorf("got %d ideas for upTo=0", len(ideas))
	}
}
