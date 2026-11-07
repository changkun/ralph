package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/changkun/ralph/internal/prompt"
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

func TestClaudeRunArchivist(t *testing.T) {
	fakeBin(t, "claude", `echo '{"result":"documented"}'`)
	raw, err := (&Claude{}).RunArchivist(context.Background(), t.TempDir(), testPrompt)
	if err != nil {
		t.Fatal(err)
	}
	if r := ExtractResult(raw); r != "documented" {
		t.Errorf("got %q", r)
	}
}

func TestClaudeRunArchivistErr(t *testing.T) {
	fakeBin(t, "claude", `exit 1`)
	_, err := (&Claude{}).RunArchivist(context.Background(), t.TempDir(), testPrompt)
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

func TestClaudeRunFailure(t *testing.T) {
	fakeBin(t, "claude", `echo '{"result":"Not logged in"}'; exit 1`)
	_, err := (&Claude{}).RunWorker(context.Background(), t.TempDir(), testPrompt)
	if err == nil || !strings.Contains(err.Error(), "Not logged in") {
		t.Errorf("err = %v, want it to include the reported message", err)
	}
}

func TestClaudeRunFailureStderr(t *testing.T) {
	fakeBin(t, "claude", `echo 'command not understood' >&2; exit 2`)
	_, err := (&Claude{}).RunWorker(context.Background(), t.TempDir(), testPrompt)
	if err == nil || !strings.Contains(err.Error(), "command not understood") {
		t.Errorf("err = %v, want it to include stderr", err)
	}
}

func TestClaudeRunSubprocessHoldsOutput(t *testing.T) {
	// The CLI succeeds but leaves a background process holding its stdout.
	fakeBin(t, "claude", `sleep 10 & echo '{"result":"ok"}'`)
	start := time.Now()
	raw, err := (&Claude{}).RunWorker(context.Background(), t.TempDir(), testPrompt)
	if err != nil {
		t.Fatal(err)
	}
	if r := ExtractResult(raw); r != "ok" {
		t.Errorf("result = %q, want %q", r, "ok")
	}
	if d := time.Since(start); d > waitDelay+5*time.Second {
		t.Errorf("Run took %v", d)
	}
}

func TestClaudeRunCancelled(t *testing.T) {
	// The CLI's child keeps running, and holding stdout, after the CLI is killed.
	fakeBin(t, "claude", `sleep 10`)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := (&Claude{}).RunWorker(ctx, t.TempDir(), testPrompt); err == nil {
		t.Error("expected an error from a cancelled run")
	}
	if d := time.Since(start); d > waitDelay+5*time.Second {
		t.Errorf("Run took %v after cancellation", d)
	}
}
