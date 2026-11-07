package backend

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/changkun/ralph/internal/transcript"
)

// fakeCLI installs a shell script called name at the front of PATH. The
// script saves its arguments, which args returns, and then runs body.
func fakeCLI(t *testing.T, name, body string) (args func() []string) {
	t.Helper()
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\0' \"$@\" > '" + argsFile + "'\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() []string {
		t.Helper()
		b, err := os.ReadFile(argsFile)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00")
	}
}

// replay returns a fake CLI body that prints a captured stream from the
// transcript package's testdata.
func replay(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "transcript", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return "cat '" + path + "'"
}

func assertArgs(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("args:\n got %q\nwant %q", got, want)
	}
}

// isolatedEnv runs commands on the host but reports itself isolated, as a
// container does, and records the readOnly flag of each command.
type isolatedEnv struct{ readOnly []bool }

func (e *isolatedEnv) Command(ctx context.Context, readOnly bool, dir, name string, args ...string) *exec.Cmd {
	e.readOnly = append(e.readOnly, readOnly)
	return Host{}.Command(ctx, readOnly, dir, name, args...)
}

func (*isolatedEnv) Isolated() bool { return true }

// recorder collects what a turn streams.
type recorder struct {
	lines  strings.Builder
	events []transcript.Event
}

func (r *recorder) turn(t Turn) Turn {
	t.Transcript = &r.lines
	t.OnEvent = func(e transcript.Event) { r.events = append(r.events, e) }
	return t
}

func (r *recorder) kinds() []transcript.Kind {
	var ks []transcript.Kind
	for _, e := range r.events {
		ks = append(ks, e.Kind)
	}
	return ks
}

func TestNew(t *testing.T) {
	env := &isolatedEnv{}
	if c, ok := New("claude", env).(*Claude); !ok || c.Env != env {
		t.Errorf(`New("claude", env) = %#v`, c)
	}
	if c, ok := New("codex", nil).(*Codex); !ok || c.Env != nil {
		t.Errorf(`New("codex", nil) = %#v`, c)
	}
	if be := New("other", nil); be != nil {
		t.Errorf(`New("other") = %v, want nil`, be)
	}
	for _, name := range Names {
		if New(name, nil) == nil {
			t.Errorf("Names lists %q, which New does not accept", name)
		}
	}
}

func TestReadOnly(t *testing.T) {
	for _, r := range []Role{Strategist, Executor, Evaluator, Archivist, Standalone} {
		if got, want := r.ReadOnly(), r == Strategist; got != want {
			t.Errorf("%s.ReadOnly() = %v, want %v", r, got, want)
		}
	}
}

func TestRunError(t *testing.T) {
	base := errors.New("exit status 1")
	tests := []struct {
		name    string
		failure string
		stderr  string
		want    string
	}{
		{"plain", "", "", "cli: exit status 1"},
		{"failure", "not logged in", "", "cli: exit status 1: not logged in"},
		{"stderr last line", "", "progress\nboom\n", "cli: exit status 1: boom"},
		{"failure wins over stderr", "not logged in", "boom", "cli: exit status 1: not logged in"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runError("cli", base, tt.failure, []byte(tt.stderr))
			if err.Error() != tt.want {
				t.Errorf("got %q, want %q", err, tt.want)
			}
			if !errors.Is(err, base) {
				t.Error("error does not wrap the original")
			}
		})
	}
}

func TestLineWriter(t *testing.T) {
	var lines []string
	w := &lineWriter{fn: func(b []byte) { lines = append(lines, string(b)) }}
	for _, chunk := range []string{"a", "b\nc\r\n", "\n", "d\ne"} {
		w.Write([]byte(chunk))
	}
	w.flush()
	if want := []string{"ab", "c", "d", "e"}; !slices.Equal(lines, want) {
		t.Errorf("lines = %q, want %q", lines, want)
	}
}

func TestTailBuffer(t *testing.T) {
	b := &tailBuffer{max: 4}
	b.Write([]byte("abc"))
	b.Write([]byte("def"))
	if got := string(b.Bytes()); got != "cdef" {
		t.Errorf("tail = %q, want %q", got, "cdef")
	}
}

func TestHost(t *testing.T) {
	dir := t.TempDir()
	cmd := Host{}.Command(context.Background(), true, dir, "pwd")
	if cmd.Dir != dir || (Host{}).Isolated() {
		t.Errorf("Host command dir = %q, isolated = %v", cmd.Dir, Host{}.Isolated())
	}
}
