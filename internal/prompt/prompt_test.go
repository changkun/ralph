package prompt

import (
	"strings"
	"testing"
	"text/template"
)

func TestThinkerPrompt(t *testing.T) {
	p := ThinkerPrompt()
	if p.User == "" {
		t.Error("missing user prompt")
	}
}

func TestWorkerPrompt(t *testing.T) {
	p := WorkerPrompt("/tmp/project", "build a CLI")
	if !strings.Contains(p.User, "/tmp/project") {
		t.Error("missing folder")
	}
	if !strings.Contains(p.User, "build a CLI") {
		t.Error("missing idea")
	}
	if p.System == "" {
		t.Error("missing system prompt")
	}
}

func TestCommitPrompt(t *testing.T) {
	p := CommitPrompt("add feature X", "added new feature")
	if !strings.Contains(p.User, "add feature X") {
		t.Error("missing objective")
	}
	if !strings.Contains(p.User, "added new feature") {
		t.Error("missing worker result")
	}
}

func TestBuilderPrompt(t *testing.T) {
	p := BuilderPrompt("/tmp/project")
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
