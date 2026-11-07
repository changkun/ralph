//go:build unix

package session

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLaunch(t *testing.T) {
	folder := t.TempDir()
	exe := filepath.Join(t.TempDir(), "engine")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\necho \"args: $*\"\nsleep 1\necho done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pid, err := Launch(exe, folder)
	if err != nil {
		t.Fatal(err)
	}
	if !Alive(pid) {
		t.Error("the engine is not running")
	}
	if pgid, err := syscall.Getpgid(pid); err == nil && pgid == syscall.Getpgrp() {
		t.Error("the engine is in this process's group")
	}
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(EngineLog(folder, 1<<10), "done") {
		if time.Now().After(deadline) {
			t.Fatalf("engine log = %q", EngineLog(folder, 1<<10))
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got, want := EngineLog(folder, 1<<10), "args: engine "+folder+"\ndone\n"; got != want {
		t.Errorf("engine log = %q, want %q", got, want)
	}
}

func TestLaunchFailures(t *testing.T) {
	if _, err := Launch(filepath.Join(t.TempDir(), "missing"), t.TempDir()); err == nil {
		t.Error("Launch of a missing program succeeded")
	}
	if _, err := Launch("/bin/sh", "/dev/null"); err == nil {
		t.Error("Launch on a file succeeded")
	}
}
