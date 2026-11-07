package prompt

import (
	"strings"
	"testing"
)

var round = Round{
	Folder:          "/tmp/project",
	Objective:       "add feature X",
	ExecutorResult:  "added feature X",
	EvaluatorResult: "tests passed",
}

func TestDefaults(t *testing.T) {
	tests := []struct {
		role     string
		wantUser []string
	}{
		{"strategist", []string{"ONE objective"}},
		{"executor", []string{round.Folder, round.Objective}},
		{"evaluator", []string{round.Folder, round.Objective, round.ExecutorResult}},
		{"archivist", []string{round.Folder, round.Objective, round.ExecutorResult, round.EvaluatorResult, "AGENTS.md"}},
		{"standalone", []string{round.Folder}},
	}
	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			p, err := ForRole(tt.role).Render(round)
			if err != nil {
				t.Fatal(err)
			}
			if p.System == "" {
				t.Error("empty system prompt")
			}
			for _, want := range tt.wantUser {
				if !strings.Contains(p.User, want) {
					t.Errorf("user prompt does not contain %q:\n%s", want, p.User)
				}
			}
		})
	}
}

// TestDefaultTextRoundTrips checks that the editable text of each built-in
// template renders exactly like the template itself.
func TestDefaultTextRoundTrips(t *testing.T) {
	for role := range defaults {
		t.Run(role, func(t *testing.T) {
			text, ok := Default(role)
			if !ok {
				t.Fatal("no default text")
			}
			if strings.Contains(text.Prompt, "{{define") || strings.Contains(text.Prompt, "{{end}}") {
				t.Errorf("text includes template definitions:\n%s", text.Prompt)
			}
			parsed, err := Parse(text)
			if err != nil {
				t.Fatal(err)
			}
			got, err := parsed.Render(round)
			if err != nil {
				t.Fatal(err)
			}
			want, err := ForRole(role).Render(round)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("rendered differently:\n got %+v\nwant %+v", got, want)
			}
		})
	}
}

func TestParse(t *testing.T) {
	p, err := Parse(Text{System: "Be brief.", Prompt: "Work on {{.Objective}} in {{.Folder}}."})
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Render(round)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Prompt{System: "Be brief.", User: "Work on add feature X in /tmp/project."}); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestParseErrors(t *testing.T) {
	for _, text := range []Text{
		{Prompt: "{{.Objective"},
		{System: "{{if}}", Prompt: "ok"},
		{Prompt: "{{.Goal}}"},
	} {
		if _, err := Parse(text); err == nil {
			t.Errorf("Parse(%+v) succeeded, want an error", text)
		}
	}
}

func TestUnknownRole(t *testing.T) {
	if _, ok := Default("planner"); ok {
		t.Error("Default reported a template for an unknown role")
	}
	defer func() {
		if recover() == nil {
			t.Error("ForRole did not panic for an unknown role")
		}
	}()
	ForRole("planner")
}
