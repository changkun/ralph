package prompt

import (
	"bytes"
	"embed"
	"math"
	"math/rand/v2"
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
)

// ExploreScore returns a value in [0, 1] representing how exploratory the
// thinker should be. Combines a sine wave (~6-round cycle) with random jitter.
func ExploreScore(round int) float64 {
	wave := (math.Sin(float64(round)*2*math.Pi/6-math.Pi/2) + 1) / 2
	jitter := (rand.Float64() - 0.5) * 0.4
	return max(0, min(1, wave+jitter))
}

// ThinkerPrompt creates a thinker prompt for a given round and score.
func ThinkerPrompt(round int, score float64) Prompt {
	return thinkerAgent.render(struct {
		Round          int
		ExplorePercent int
		Score          float64
	}{round, int(score * 100), score})
}

// ThinkerPromptForRound generates the thinker prompt with a dynamic explore score.
func ThinkerPromptForRound(round int) Prompt {
	return ThinkerPrompt(round, ExploreScore(round))
}

// WorkerPrompt formats the worker prompt with folder and idea.
func WorkerPrompt(folder, idea string) Prompt {
	return workerAgent.render(struct{ Folder, Idea string }{folder, idea})
}

// CommitPrompt formats the committer prompt with objective and worker result.
func CommitPrompt(objective, workerResult string) Prompt {
	return committerAgent.render(struct{ Objective, WorkerResult string }{objective, workerResult})
}

