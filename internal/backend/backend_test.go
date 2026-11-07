package backend

import (
	"errors"
	"os/exec"
	"testing"
)

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
	if be := New("claude"); be == nil {
		t.Error("expected Claude backend")
	}
	if be := New("codex"); be == nil {
		t.Error("expected Codex backend")
	}
	if be := New("other"); be != nil {
		t.Error("expected nil")
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
