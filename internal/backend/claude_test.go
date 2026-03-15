package backend

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"changkun.de/ralph/internal/prompt"
)

func fakeBin(t *testing.T, name, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

var testPrompt = prompt.Prompt{System: "sys", User: "test"}

func TestClaudeRunThinker(t *testing.T) {
	fakeBin(t, "claude", `echo '{"result":"idea"}'`)
	raw, err := (&Claude{}).RunThinker(context.Background(), t.TempDir(), testPrompt)
	if err != nil {
		t.Fatal(err)
	}
	if r := ExtractResult(raw); r != "idea" {
		t.Errorf("got %q", r)
	}
}

func TestClaudeRunWorker(t *testing.T) {
	fakeBin(t, "claude", `echo '{"result":"done"}'`)
	raw, err := (&Claude{}).RunWorker(context.Background(), t.TempDir(), testPrompt)
	if err != nil {
		t.Fatal(err)
	}
	if r := ExtractResult(raw); r != "done" {
		t.Errorf("got %q", r)
	}
}

func TestClaudeRunCommitter(t *testing.T) {
	fakeBin(t, "claude", `echo '{"result":"committed"}'`)
	r, err := (&Claude{}).RunCommitter(context.Background(), t.TempDir(), testPrompt)
	if err != nil {
		t.Fatal(err)
	}
	if r != "committed" {
		t.Errorf("got %q", r)
	}
}

func TestClaudeRunCommitterErr(t *testing.T) {
	fakeBin(t, "claude", `exit 1`)
	_, err := (&Claude{}).RunCommitter(context.Background(), t.TempDir(), testPrompt)
	if err == nil {
		t.Error("expected error")
	}
}

func TestClaudeNoSystem(t *testing.T) {
	fakeBin(t, "claude", `echo '{"result":"ok"}'`)
	p := prompt.Prompt{User: "test"}
	raw, err := (&Claude{}).RunThinker(context.Background(), t.TempDir(), p)
	if err != nil {
		t.Fatal(err)
	}
	if r := ExtractResult(raw); r != "ok" {
		t.Errorf("got %q", r)
	}
}
