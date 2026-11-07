// Package prompt renders the prompt for each agent role. Every role has a
// built-in template in templates/, which defines a "system" block, added to
// the agent's system prompt, and a "prompt" block, the task message. Both
// can be replaced, for example from the GUI, with Parse.
package prompt

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed templates/*.tmpl
var templates embed.FS

// Prompt is a rendered prompt for one agent turn.
type Prompt struct {
	System string // added to the agent's system prompt; may be empty
	User   string // the task message
}

// Round is what an agent is told about the current round. Each role
// receives the fields that earlier roles in the round have filled in.
type Round struct {
	Folder          string // project directory, as the agent sees it
	Objective       string // the strategist's objective
	ExecutorResult  string // the executor's summary of its changes
	EvaluatorResult string // the evaluator's report
}

// Text is the editable source of a role's prompt: two text/template bodies
// that can use the fields of Round, such as {{.Objective}}.
type Text struct {
	System string `json:"system"`
	Prompt string `json:"prompt"`
}

// Template is a parsed prompt, ready to render.
type Template struct {
	system, prompt *template.Template
}

// Default returns the built-in prompt text for role, such as "strategist",
// and reports whether role has one.
func Default(role string) (Text, bool) {
	t, ok := defaults[role]
	if !ok {
		return Text{}, false
	}
	return Text{System: source(t, "system"), Prompt: source(t, "prompt")}, true
}

// ForRole returns the built-in template for role. It panics if role has
// none.
func ForRole(role string) *Template {
	t, ok := defaults[role]
	if !ok {
		panic("prompt: no template for role " + role)
	}
	return &Template{system: t.Lookup("system"), prompt: t.Lookup("prompt")}
}

// Parse parses text. It fails if either part is not a valid template or
// refers to something Round does not have.
func Parse(text Text) (*Template, error) {
	system, err := template.New("system").Parse(text.System)
	if err != nil {
		return nil, err
	}
	user, err := template.New("prompt").Parse(text.Prompt)
	if err != nil {
		return nil, err
	}
	t := &Template{system: system, prompt: user}
	if _, err := t.Render(Round{}); err != nil {
		return nil, err
	}
	return t, nil
}

// Render renders the prompt for round r.
func (t *Template) Render(r Round) (Prompt, error) {
	system, err := execute(t.system, r)
	if err != nil {
		return Prompt{}, err
	}
	user, err := execute(t.prompt, r)
	if err != nil {
		return Prompt{}, err
	}
	return Prompt{System: system, User: user}, nil
}

func execute(t *template.Template, r Round) (string, error) {
	var buf bytes.Buffer
	if err := t.Execute(&buf, r); err != nil {
		return "", fmt.Errorf("%s template: %w", t.Name(), err)
	}
	return strings.TrimSpace(buf.String()), nil
}

// defaults holds the built-in template of each role.
var defaults = map[string]*template.Template{
	"strategist": load("strategist"),
	"executor":   load("executor"),
	"evaluator":  load("evaluator"),
	"archivist":  load("archivist"),
	"standalone": load("standalone"),
}

func load(name string) *template.Template {
	return template.Must(template.ParseFS(templates, "templates/"+name+".tmpl"))
}

// source returns the template source of the named block of t.
func source(t *template.Template, name string) string {
	return strings.TrimSpace(t.Lookup(name).Tree.Root.String())
}
