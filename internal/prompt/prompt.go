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
	strategistAgent = loadAgent("strategist")
	executorAgent   = loadAgent("executor")
	evaluatorAgent  = loadAgent("evaluator")
	archivistAgent  = loadAgent("archivist")
	standaloneAgent = loadAgent("standalone")
)

// StrategistPrompt generates the strategist prompt.
func StrategistPrompt() Prompt {
	return strategistAgent.render(nil)
}

// ExecutorPrompt formats the executor prompt with folder and objective.
func ExecutorPrompt(folder, objective string) Prompt {
	return executorAgent.render(struct{ Folder, Objective string }{folder, objective})
}

// EvaluatorPrompt formats the evaluator prompt with the latest objective and result.
func EvaluatorPrompt(folder, objective, executorResult string) Prompt {
	return evaluatorAgent.render(struct{ Folder, Objective, ExecutorResult string }{folder, objective, executorResult})
}

// MemoryFile returns the memory filename for a given backend.
func MemoryFile(backendName string) string {
	if backendName == "codex" {
		return "AGENTS.md"
	}
	return "CLAUDE.md"
}

// ArchivistPrompt formats the archivist prompt with round context.
func ArchivistPrompt(folder, objective, executorResult, evaluatorResult, memoryFile string) Prompt {
	return archivistAgent.render(struct {
		Folder, Objective, ExecutorResult, EvaluatorResult, MemoryFile string
	}{folder, objective, executorResult, evaluatorResult, memoryFile})
}

// StandalonePrompt generates the standalone prompt with the project folder.
func StandalonePrompt(folder string) Prompt {
	return standaloneAgent.render(struct{ Folder string }{folder})
}
