package backend

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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

func TestExtractResult(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
		want string
	}{
		{"valid", []byte(`{"result":"hello"}`), "hello"},
		{"empty", []byte(`{"result":""}`), ""},
		{"invalid", []byte(`not json`), ""},
		{"nil", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractResult(tt.raw); got != tt.want {
				t.Errorf("ExtractResult() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNew(t *testing.T) {
	if _, ok := New("claude").(*Claude); !ok {
		t.Error(`New("claude") is not a *Claude`)
	}
	if _, ok := New("codex").(*Codex); !ok {
		t.Error(`New("codex") is not a *Codex`)
	}
	if be := New("other"); be != nil {
		t.Errorf(`New("other") = %v, want nil`, be)
	}
}

func TestRunError(t *testing.T) {
	base := errors.New("exit status 1")
	// stderr returns the error of a command that writes s to stderr and exits 3.
	stderr := func(s string) error {
		_, err := exec.Command("sh", "-c", `printf "$0" >&2; exit 3`, s).Output()
		return err
	}
	tests := []struct {
		name    string
		err     error
		message string
		want    string
	}{
		{"plain", base, "", "cli: exit status 1"},
		{"message", base, "not logged in", "cli: exit status 1: not logged in"},
		{"stderr last line", stderr("progress\nboom\n"), "", "cli: exit status 3: boom"},
		{"message wins over stderr", stderr("boom"), "not logged in", "cli: exit status 3: not logged in"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runError("cli", tt.err, tt.message)
			if err.Error() != tt.want {
				t.Errorf("got %q, want %q", err, tt.want)
			}
			if !errors.Is(err, tt.err) {
				t.Error("error does not wrap the original")
			}
		})
	}
}

func assertArgs(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("args:\n got %q\nwant %q", got, want)
	}
}
