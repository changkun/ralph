package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/changkun/ralph/internal/backend"
	"github.com/changkun/ralph/internal/loop"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		backend string
		pattern loop.Pattern
		rounds  int
	}{
		{"defaults", []string{"/tmp"}, "claude", loop.ThinkAct, 0},
		{"codex", []string{"--backend", "codex", "/tmp"}, "codex", loop.ThinkAct, 0},
		{"max rounds", []string{"--max-rounds", "5", "/tmp"}, "claude", loop.ThinkAct, 5},
		{"options after folder", []string{"/tmp", "--max-rounds", "5"}, "claude", loop.ThinkAct, 5},
		{"standalone", []string{"--pattern", "standalone", "/tmp"}, "claude", loop.Standalone, 0},
		{"legacy pattern", []string{"--pattern", "builder", "/tmp"}, "claude", loop.Standalone, 0},
		{"full pipeline", []string{"--pattern", "think+act+evaluator+archivist", "/tmp"}, "claude", loop.ThinkActEvaluatorArchivist, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseArgs(tt.args)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.backendName != tt.backend || cfg.pattern != tt.pattern || cfg.maxRounds != tt.rounds || cfg.folder != "/tmp" {
				t.Errorf("got %+v", cfg)
			}
		})
	}
}

func TestParseArgsModel(t *testing.T) {
	cfg, err := parseArgs([]string{"--model", "opus", "/tmp"})
	if err != nil || cfg.model != "opus" {
		t.Errorf("model = %q, err = %v; want opus", cfg.model, err)
	}
}

func TestParseArgsErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no folder", nil},
		{"two folders", []string{"/a", "/b"}},
		{"unknown option", []string{"--foo", "/tmp"}},
		{"unknown backend", []string{"--backend", "gpt", "/tmp"}},
		{"unknown pattern", []string{"--pattern", "bad", "/tmp"}},
		{"backend without value", []string{"--backend"}},
		{"model without value", []string{"--model"}},
		{"pattern without value", []string{"--pattern"}},
		{"max rounds without value", []string{"--max-rounds"}},
		{"max rounds not a number", []string{"--max-rounds", "abc", "/tmp"}},
		{"max rounds trailing junk", []string{"--max-rounds", "5x", "/tmp"}},
		{"max rounds negative", []string{"--max-rounds", "-1", "/tmp"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseArgs(tt.args); err == nil || errors.Is(err, errHelp) {
				t.Errorf("err = %v, want a usage error", err)
			}
		})
	}
}

func TestParseArgsHelp(t *testing.T) {
	for _, arg := range []string{"-h", "--help"} {
		if _, err := parseArgs([]string{arg}); !errors.Is(err, errHelp) {
			t.Errorf("parseArgs(%q) err = %v, want errHelp", arg, err)
		}
	}
}

// stubBackend answers every role with "ok", or with ctx's error once ctx is
// cancelled.
type stubBackend struct{}

func (stubBackend) Run(ctx context.Context, _ backend.Turn) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "ok", nil
}

func TestRun(t *testing.T) {
	for _, p := range loop.Patterns {
		t.Run(string(p), func(t *testing.T) {
			dir := t.TempDir()
			if err := run(context.Background(), config{backendName: "claude", pattern: p, maxRounds: 1, folder: dir}, stubBackend{}); err != nil {
				t.Fatal(err)
			}
			if got := loop.ResumeRound(filepath.Join(dir, ".ralph"), p); got != 1 {
				t.Errorf("finished rounds = %d, want 1", got)
			}
		})
	}
}

func TestRunInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := run(ctx, config{backendName: "claude", pattern: loop.ThinkAct, folder: t.TempDir()}, stubBackend{}); err != nil {
		t.Fatal(err)
	}
}

func TestRunResumes(t *testing.T) {
	dir := t.TempDir()
	ralphDir := filepath.Join(dir, ".ralph")
	if err := os.MkdirAll(ralphDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		for _, role := range []string{"strategist", "executor"} {
			if err := os.WriteFile(filepath.Join(ralphDir, fmt.Sprintf("round-%03d-%s.json", i, role)), []byte(`{}`), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := run(context.Background(), config{backendName: "claude", pattern: loop.ThinkAct, maxRounds: 4, folder: dir}, stubBackend{}); err != nil {
		t.Fatal(err)
	}
	if got := loop.ResumeRound(ralphDir, loop.ThinkAct); got != 4 {
		t.Errorf("finished rounds = %d, want 4", got)
	}
}

func TestRunCannotCreateLogDir(t *testing.T) {
	if err := run(context.Background(), config{pattern: loop.ThinkAct, folder: "/dev/null"}, stubBackend{}); err == nil {
		t.Error("expected an error")
	}
}

func callMain(t *testing.T, args []string, want int) {
	t.Helper()
	origArgs, origExit := os.Args, osExit
	defer func() { os.Args, osExit = origArgs, origExit }()
	code := -1
	osExit = func(c int) { code = c }
	os.Args = append([]string{"ralph"}, args...)
	main()
	if code != want {
		t.Errorf("exit code = %d, want %d", code, want)
	}
}

func TestMainUsageError(t *testing.T) { callMain(t, nil, 1) }
func TestMainHelp(t *testing.T)       { callMain(t, []string{"--help"}, 0) }
func TestMainRunError(t *testing.T)   { callMain(t, []string{"/dev/null"}, 1) }

func TestMainSuccess(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\necho '{\"type\":\"result\",\"result\":\"x\"}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	callMain(t, []string{"--max-rounds", "1", t.TempDir()}, 0)
}

func TestMainAgentFailure(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	callMain(t, []string{"--max-rounds", "1", t.TempDir()}, 1)
}
