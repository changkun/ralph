package backend

import (
	"context"
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
	raw, err := (&Codex{}).RunThinker(context.Background(), t.TempDir(), testPrompt)
	if err != nil {
		t.Fatal(err)
	}
	if r := ExtractResult(raw); r != "" {
		t.Errorf("got %q, want empty", r)
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
