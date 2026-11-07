package prompt

import (
	"strings"
	"testing"
	"text/template"
)

var round = Round{
	Folder:          "/tmp/project",
	Objective:       "add feature X",
	ExecutorResult:  "added feature X",
	EvaluatorResult: "tests passed",
	MemoryFile:      "CLAUDE.md",
}

func TestPrompts(t *testing.T) {
	tests := []struct {
		name       string
		p          Prompt
		wantSystem bool
		wantUser   []string
	}{
		{"strategist", Strategist(), false, []string{"ONE objective"}},
		{"executor", Executor(round), true, []string{round.Folder, round.Objective}},
		{"evaluator", Evaluator(round), true, []string{round.Folder, round.Objective, round.ExecutorResult}},
		{"archivist", Archivist(round), true, []string{round.Folder, round.Objective, round.ExecutorResult, round.EvaluatorResult, round.MemoryFile}},
		{"standalone", Standalone(round.Folder), true, []string{round.Folder}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.System != ""; got != tt.wantSystem {
				t.Errorf("has system prompt = %v, want %v", got, tt.wantSystem)
			}
			for _, want := range tt.wantUser {
				if !strings.Contains(tt.p.User, want) {
					t.Errorf("user prompt does not contain %q:\n%s", want, tt.p.User)
				}
			}
		})
	}
}

func TestRenderPanicsOnTemplateError(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected a panic")
		}
	}()
	bad := template.Must(template.New("bad").Parse(
		`{{define "system"}}ok{{end}}{{define "prompt"}}{{.Missing.Field}}{{end}}`))
	render(bad, "not a struct")
}

func TestLoadPanicsOnMissingTemplate(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected a panic")
		}
	}()
	load("nonexistent")
}
