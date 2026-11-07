package session

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestEngine(t *testing.T) {
	isolate(t)
	fakeBin(t, map[string]string{"claude": fakeClaude})
	folder := t.TempDir()
	if err := hostSettings().Save(folder); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := Engine(folder, &out); err != nil {
		t.Fatal(err)
	}
	if st, _ := ReadStatus(folder); st.State != Finished {
		t.Errorf("status = %+v\n%s", st, out.String())
	}
	if err := writeFile(configPath(folder), []byte("{")); err != nil {
		t.Fatal(err)
	}
	if err := Engine(folder, &out); err == nil {
		t.Error("Engine ran with a malformed config")
	}
}

func TestEngineLog(t *testing.T) {
	folder := t.TempDir()
	if got := EngineLog(folder, 100); got != "" {
		t.Errorf("EngineLog without a log = %q", got)
	}
	if err := writeFile(engineLogPath(folder), []byte("first line\nsecond\nthird\n")); err != nil {
		t.Fatal(err)
	}
	if got := EngineLog(folder, 100); got != "first line\nsecond\nthird\n" {
		t.Errorf("whole log = %q", got)
	}
	if got := EngineLog(folder, 12); got != "third\n" {
		t.Errorf("log tail = %q, want the last whole line", got)
	}
}

func TestAlive(t *testing.T) {
	if !Alive(os.Getpid()) {
		t.Error("this process is not alive")
	}
	for _, pid := range []int{0, -1, 1 << 30} {
		if Alive(pid) {
			t.Errorf("Alive(%d) = true", pid)
		}
	}
}

func TestEngineInterrupted(t *testing.T) {
	isolate(t)
	// The agent runs until the engine is interrupted.
	fakeBin(t, map[string]string{"claude": "sleep 30"})
	folder := t.TempDir()
	if err := hostSettings().Save(folder); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- Engine(folder, &strings.Builder{}) }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if st, _ := ReadStatus(folder); st.Role != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the session did not start a turn")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := Stop(folder); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the engine did not stop")
	}
	if st, _ := ReadStatus(folder); st.State != Stopped {
		t.Errorf("status = %+v, want stopped", st)
	}
}
