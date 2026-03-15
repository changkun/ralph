package prompt

import (
	"bytes"
	"embed"
	"strings"
	"text/template"
)

//go:embed templates/*.tmpl
var tmplFS embed.FS

// Prompt holds rendered system and user prompt strings.
type Prompt struct {
	System string
	User   string
}

// agentTmpl wraps a parsed template for one agent.
type agentTmpl struct {
	t *template.Template
}

func loadAgent(name string) agentTmpl {
	data, err := tmplFS.ReadFile("templates/" + name + ".tmpl")
	if err != nil {
		panic("missing template: " + name)
	}
	return agentTmpl{t: template.Must(template.New(name).Parse(string(data)))}
}

func (a agentTmpl) render(data any) Prompt {
	exec := func(block string) string {
		var buf bytes.Buffer
		if err := a.t.ExecuteTemplate(&buf, block, data); err != nil {
			return ""
		}
		return strings.TrimSpace(buf.String())
	}
	return Prompt{System: exec("system"), User: exec("prompt")}
}

var (
	thinkerAgent   = loadAgent("thinker")
	workerAgent    = loadAgent("worker")
	committerAgent = loadAgent("committer")
	builderAgent   = loadAgent("builder")
)

// ThinkerPrompt generates the thinker prompt.
func ThinkerPrompt() Prompt {
	return thinkerAgent.render(nil)
}

// WorkerPrompt formats the worker prompt with folder and idea.
func WorkerPrompt(folder, idea string) Prompt {
	return workerAgent.render(struct{ Folder, Idea string }{folder, idea})
}

// CommitPrompt formats the committer prompt with objective and worker result.
func CommitPrompt(objective, workerResult string) Prompt {
	return committerAgent.render(struct{ Objective, WorkerResult string }{objective, workerResult})
}

// BuilderPrompt generates the builder prompt with the project folder.
func BuilderPrompt(folder string) Prompt {
	return builderAgent.render(struct{ Folder string }{folder})
}
