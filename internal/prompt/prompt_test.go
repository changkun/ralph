package prompt

import (
	"strings"
	"testing"
	"text/template"
)

func TestStrategistPrompt(t *testing.T) {
	p := StrategistPrompt()
	if p.User == "" {
		t.Error("missing user prompt")
	}
}

func TestExecutorPrompt(t *testing.T) {
	p := ExecutorPrompt("/tmp/project", "build a CLI")
	if !strings.Contains(p.User, "/tmp/project") {
		t.Error("missing folder")
	}
	if !strings.Contains(p.User, "build a CLI") {
		t.Error("missing objective")
	}
	if p.System == "" {
		t.Error("missing system prompt")
	}
}

func TestEvaluatorPrompt(t *testing.T) {
	p := EvaluatorPrompt("/tmp/project", "add feature X", "added new feature")
	if !strings.Contains(p.User, "/tmp/project") {
		t.Error("missing folder")
	}
	if !strings.Contains(p.User, "add feature X") {
		t.Error("missing objective")
	}
	if !strings.Contains(p.User, "added new feature") {
		t.Error("missing executor result")
	}
}

func TestArchivistPrompt(t *testing.T) {
	p := ArchivistPrompt("/tmp/project", "add feature X", "added new feature", "tests passed", "CLAUDE.md")
	if !strings.Contains(p.User, "/tmp/project") {
		t.Error("missing folder")
	}
	if !strings.Contains(p.User, "add feature X") {
		t.Error("missing objective")
	}
	if !strings.Contains(p.User, "added new feature") {
		t.Error("missing executor result")
	}
	if !strings.Contains(p.User, "tests passed") {
		t.Error("missing evaluator result")
	}
	if !strings.Contains(p.User, "CLAUDE.md") {
		t.Error("missing memory file")
	}
}

func TestStandalonePrompt(t *testing.T) {
	p := StandalonePrompt("/tmp/project")
	if !strings.Contains(p.User, "/tmp/project") {
		t.Error("missing folder")
	}
	if p.System == "" {
		t.Error("missing system prompt")
	}
}

func TestRenderError(t *testing.T) {
	at := agentTmpl{t: template.Must(template.New("bad").Parse(
		`{{define "system"}}ok{{end}}{{define "prompt"}}{{.Missing.Field}}{{end}}`))}
	p := at.render("not a struct")
	if p.User != "" {
		t.Errorf("expected empty user prompt on error, got %q", p.User)
	}
}

func TestLoadAgentPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	loadAgent("nonexistent_agent")
}
