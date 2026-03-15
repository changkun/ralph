package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/changkun/ralph/internal/backend"
	"github.com/changkun/ralph/internal/prompt"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
		be      string
		rounds  int
	}{
		{"default", []string{"/tmp"}, false, "claude", 0},
		{"codex", []string{"--backend", "codex", "/tmp"}, false, "codex", 0},
		{"rounds", []string{"--max-rounds", "5", "/tmp"}, false, "claude", 5},
		{"no folder", []string{}, true, "", 0},
		{"bad backend", []string{"--backend", "gpt", "/tmp"}, true, "", 0},
		{"unknown flag", []string{"--foo"}, true, "", 0},
		{"backend no val", []string{"--backend"}, true, "", 0},
		{"rounds no val", []string{"--max-rounds"}, true, "", 0},
		{"rounds bad", []string{"--max-rounds", "abc", "/tmp"}, true, "", 0},
		{"rounds neg", []string{"--max-rounds", "-1", "/tmp"}, true, "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseArgs(tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, tt.wantErr)
			}
			if err == nil {
				if cfg.backendName != tt.be {
					t.Errorf("backend=%q", cfg.backendName)
				}
				if cfg.maxRounds != tt.rounds {
					t.Errorf("rounds=%d", cfg.maxRounds)
				}
			}
		})
	}
}

type runMock struct{}

func (m *runMock) RunThinker(ctx context.Context, _ string, _ prompt.Prompt) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return []byte(`{"result":"idea"}`), nil
}
func (m *runMock) RunWorker(ctx context.Context, _ string, _ prompt.Prompt) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return []byte(`{"result":"done"}`), nil
}
func (m *runMock) RunCommitter(_ context.Context, _ string, _ prompt.Prompt) (string, error) {
	return "", nil
}

var _ backend.Backend = (*runMock)(nil)

func TestRun(t *testing.T) {
	err := run(context.Background(), config{backendName: "claude", maxRounds: 1, folder: t.TempDir()}, &runMock{})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunUnlimited(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := run(ctx, config{backendName: "claude", maxRounds: 0, folder: t.TempDir()}, &runMock{}); err != nil {
		t.Fatal(err)
	}
}

func TestRunResume(t *testing.T) {
	dir := t.TempDir()
	rd := filepath.Join(dir, ".ralph")
	os.MkdirAll(rd, 0o755)
	for i := 1; i <= 3; i++ {
		data, _ := json.Marshal(backend.Result{Value: fmt.Sprintf("idea%d", i)})
		os.WriteFile(filepath.Join(rd, fmt.Sprintf("round-%03d-thinker.json", i)), data, 0o644)
		os.WriteFile(filepath.Join(rd, fmt.Sprintf("round-%03d-worker.json", i)), data, 0o644)
	}
	if err := run(context.Background(), config{backendName: "claude", maxRounds: 4, folder: dir}, &runMock{}); err != nil {
		t.Fatal(err)
	}
}

func TestRunMkdirErr(t *testing.T) {
	if err := run(context.Background(), config{folder: "/dev/null"}, &runMock{}); err == nil {
		t.Error("expected error")
	}
}

func callMain(t *testing.T, args []string, want int) {
	t.Helper()
	orig, origExit := os.Args, osExit
	defer func() { os.Args, osExit = orig, origExit }()
	var code int
	osExit = func(c int) { code = c; panic("exit") }
	os.Args = append([]string{"ralph"}, args...)
	func() { defer func() { recover() }(); main() }()
	if code != want {
		t.Errorf("exit code = %d, want %d", code, want)
	}
}

func TestMainParseErr(t *testing.T) { callMain(t, nil, 1) }
func TestMainRunErr(t *testing.T)   { callMain(t, []string{"/dev/null"}, 1) }
func TestMainSuccess(t *testing.T) {
	dir := t.TempDir()
	fakeDir := t.TempDir()
	os.WriteFile(filepath.Join(fakeDir, "claude"), []byte("#!/bin/sh\necho '{\"result\":\"x\"}'"), 0o755)
	t.Setenv("PATH", fakeDir+":"+os.Getenv("PATH"))
	callMain(t, []string{"--max-rounds", "1", dir}, 0)
}
