package backend

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/changkun/ralph/internal/prompt"
)

const codexScript = `while [ $# -gt 0 ]; do case "$1" in -o) echo "output" > "$2"; shift 2;; *) shift;; esac; done`

func TestCodexRunThinker(t *testing.T) {
	fakeBin(t, "codex", codexScript)
	raw, err := (&Codex{}).RunThinker(context.Background(), t.TempDir(), testPrompt)
	if err != nil {
		t.Fatal(err)
	}
	if r := ExtractResult(raw); r != "output\n" {
		t.Errorf("got %q", r)
	}
}

func TestCodexRunWorker(t *testing.T) {
	fakeBin(t, "codex", codexScript)
	raw, err := (&Codex{}).RunWorker(context.Background(), t.TempDir(), testPrompt)
	if err != nil {
		t.Fatal(err)
	}
	if r := ExtractResult(raw); r != "output\n" {
		t.Errorf("got %q", r)
	}
}

func TestCodexRunArchivist(t *testing.T) {
	fakeBin(t, "codex", codexScript)
	raw, err := (&Codex{}).RunArchivist(context.Background(), t.TempDir(), testPrompt)
	if err != nil {
		t.Fatal(err)
	}
	if r := ExtractResult(raw); r != "output\n" {
		t.Errorf("got %q", r)
	}
}

func TestCodexCreateTempErr(t *testing.T) {
	t.Setenv("TMPDIR", "/nonexistent_ralph_test_dir")
	_, err := (&Codex{}).RunThinker(context.Background(), "/dummy", testPrompt)
	if err == nil {
		t.Error("expected error")
	}
}

func TestCodexArchivistCreateTempErr(t *testing.T) {
	t.Setenv("TMPDIR", "/nonexistent_ralph_test_dir")
	_, err := (&Codex{}).RunArchivist(context.Background(), "/dummy", testPrompt)
	if err == nil {
		t.Error("expected error")
	}
}

func TestCodexReadFileErr(t *testing.T) {
	fakeBin(t, "codex", `while [ $# -gt 0 ]; do case "$1" in -o) rm -f "$2"; shift 2;; *) shift;; esac; done`)
	if _, err := (&Codex{}).RunThinker(context.Background(), t.TempDir(), testPrompt); err == nil {
		t.Error("expected an error when codex leaves no output file")
	}
}

func TestCodexRunFailure(t *testing.T) {
	fakeBin(t, "codex", `echo 'error: unexpected argument' >&2; exit 2`)
	_, err := (&Codex{}).RunWorker(context.Background(), t.TempDir(), testPrompt)
	if err == nil || !strings.Contains(err.Error(), "unexpected argument") {
		t.Errorf("err = %v, want it to include stderr", err)
	}
}

func TestCodexRunCancelled(t *testing.T) {
	fakeBin(t, "codex", codexScript)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (&Codex{}).RunWorker(ctx, t.TempDir(), testPrompt); err == nil {
		t.Error("expected an error from a cancelled run")
	}
}

func TestCodexNoSystem(t *testing.T) {
	fakeBin(t, "codex", codexScript)
	p := prompt.Prompt{User: "test"}
	raw, err := (&Codex{}).RunThinker(context.Background(), t.TempDir(), p)
	if err != nil {
		t.Fatal(err)
	}
	if r := ExtractResult(raw); r != "output\n" {
		t.Errorf("got %q", r)
	}
}

func TestCodexArgs(t *testing.T) {
	tests := []struct {
		name string
		run  func(*Codex) ([]byte, error)
		want []string
	}{
		{"thinker", func(c *Codex) ([]byte, error) { return c.RunThinker(context.Background(), "/project", testPrompt) },
			[]string{"exec", "--sandbox", "workspace-write", "--ephemeral", "-C", "/project", "-o", "<out>", "sys\n\ntest"}},
		{"worker", func(c *Codex) ([]byte, error) { return c.RunWorker(context.Background(), "/project", testPrompt) },
			[]string{"exec", "--sandbox", "workspace-write", "-C", "/project", "-o", "<out>", "sys\n\ntest"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argsFile := filepath.Join(t.TempDir(), "args")
			fakeBin(t, "codex", `printf '%s\0' "$@" > '`+argsFile+`'`)
			if _, err := tt.run(&Codex{}); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(argsFile)
			if err != nil {
				t.Fatal(err)
			}
			args := strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00")
			if i := slices.Index(args, "-o"); i >= 0 && i+1 < len(args) {
				args[i+1] = "<out>" // a random temporary file
			}
			if !slices.Equal(args, tt.want) {
				t.Errorf("args:\n got %q\nwant %q", args, tt.want)
			}
		})
	}
}
