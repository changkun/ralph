// Package prompt renders the prompt for each agent role from the templates
// in templates/. Each template defines a "system" block, appended to the
// agent's system prompt, and a "prompt" block, the task message.
package prompt

import (
	"bytes"
	"embed"
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
	Folder          string // project directory
	Objective       string // the strategist's objective
	ExecutorResult  string // the executor's summary of its changes
	EvaluatorResult string // the evaluator's report
}

var (
	strategist = load("strategist")
	executor   = load("executor")
	evaluator  = load("evaluator")
	archivist  = load("archivist")
	standalone = load("standalone")
)

// Strategist returns the prompt that asks for the next objective.
func Strategist() Prompt { return render(strategist, nil) }

// Executor returns the prompt that asks for r.Objective to be implemented.
func Executor(r Round) Prompt { return render(executor, r) }

// Evaluator returns the prompt that asks for the executor's work to be
// verified.
func Evaluator(r Round) Prompt { return render(evaluator, r) }

// Archivist returns the prompt that asks for the round's lasting knowledge
// to be recorded in AGENTS.md, which every supported agent CLI reads, and
// in the project documentation.
func Archivist(r Round) Prompt { return render(archivist, r) }

// Standalone returns the prompt that asks a single agent to choose and
// implement the next step in folder.
func Standalone(folder string) Prompt { return render(standalone, Round{Folder: folder}) }

func load(name string) *template.Template {
	return template.Must(template.ParseFS(templates, "templates/"+name+".tmpl"))
}

// render executes the system and prompt blocks of t. The templates are
// embedded and their data types fixed, so failure is a programming error.
func render(t *template.Template, data any) Prompt {
	block := func(name string) string {
		var buf bytes.Buffer
		if err := t.ExecuteTemplate(&buf, name, data); err != nil {
			panic(err)
		}
		return strings.TrimSpace(buf.String())
	}
	return Prompt{System: block("system"), User: block("prompt")}
}
