package prompt

import (
	"strings"
	"testing"
	"text/template"
)

func TestExploreScore(t *testing.T) {
	for i := 1; i <= 100; i++ {
		s := ExploreScore(i)
		if s < 0 || s > 1 {
			t.Errorf("ExploreScore(%d) = %f, want [0, 1]", i, s)
		}
	}
}

func TestThinkerPrompt(t *testing.T) {
	tests := []struct {
		name  string
		score float64
		want  string
	}{
		{"explore", 0.9, "exploration"},
		{"exploit", 0.1, "exploitation"},
		{"balanced", 0.5, "exploration and exploitation"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := ThinkerPrompt(1, tt.score)
			if !strings.Contains(p.User, tt.want) {
				t.Errorf("missing %q in prompt", tt.want)
			}
			if !strings.Contains(p.User, "round 1") {
				t.Error("missing round number")
			}
			if p.System == "" {
				t.Error("missing system prompt")
			}
		})
	}
}

func TestThinkerPromptForRound(t *testing.T) {
	p := ThinkerPromptForRound(5)
	if !strings.Contains(p.User, "round 5") {
		t.Error("missing round number")
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
	if !strings.Contains(p.User, "added new feature") {
		t.Error("missing worker result")
	}
	if p.System == "" {
		t.Error("missing system prompt")
	}
}


func TestRenderError(t *testing.T) {
	// Create a template that will fail on Execute with wrong data type.
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
