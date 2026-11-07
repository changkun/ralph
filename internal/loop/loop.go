// Package loop runs Ralph's rounds. A round runs the roles of a pattern in
// order, then commits the project and pushes it if the branch has an
// upstream.
package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/changkun/ralph/internal/backend"
	"github.com/changkun/ralph/internal/git"
	"github.com/changkun/ralph/internal/prompt"
	"github.com/changkun/ralph/internal/transcript"
)

const separator = "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

// stages names what each role is doing, for the round banners.
var stages = map[backend.Role]string{
	backend.Strategist: "Strategizing",
	backend.Executor:   "Executing",
	backend.Evaluator:  "Evaluating",
	backend.Archivist:  "Archiving",
	backend.Standalone: "Working",
}

// Agent is how a loop runs one role.
type Agent struct {
	Backend  backend.Backend
	Model    string           // empty for the backend's default model
	Template *prompt.Template // nil for the role's built-in prompt
}

// Config describes a loop.
type Config struct {
	Pattern Pattern
	Agents  map[backend.Role]Agent // an agent for each role of Pattern

	Folder      string // the project folder on this machine, which Ralph commits
	AgentFolder string // the project folder as agents see it; Folder if empty
	LogDir      string // where round logs go, usually Folder/.ralph
	MaxRounds   int    // stop after this round; 0 means no limit

	Output io.Writer                          // where progress is printed; os.Stdout if nil
	OnStep func(round int, role backend.Role) // called as each turn starts; may be nil
}

// Run runs rounds of cfg.Pattern, numbering them from finished+1, where
// finished counts the rounds completed by earlier runs (see ResumeRound).
// For each turn it saves the agent's transcript in cfg.LogDir as
// round-NNN-<role>.jsonl and its final answer as round-NNN-<role>.json.
//
// Run stops after round cfg.MaxRounds, when ctx is cancelled, or when an
// agent's CLI fails; only the last case returns an error. A round whose
// strategist or standalone agent produces no output is skipped.
func Run(ctx context.Context, cfg Config, finished int) error {
	if cfg.Output == nil {
		cfg.Output = os.Stdout
	}
	if cfg.AgentFolder == "" {
		cfg.AgentFolder = cfg.Folder
	}
	for _, role := range cfg.Pattern.Roles() {
		if cfg.Agents[role].Backend == nil {
			return fmt.Errorf("no agent configured for the %s role", role)
		}
	}
	for n := finished + 1; cfg.MaxRounds == 0 || n <= cfg.MaxRounds; n++ {
		r := round{
			cfg:       &cfg,
			num:       n,
			logPrefix: filepath.Join(cfg.LogDir, fmt.Sprintf("round-%03d", n)),
		}
		var summary string
		var err error
		if cfg.Pattern == Standalone {
			summary, err = r.step(ctx, backend.Standalone, prompt.Round{Folder: cfg.AgentFolder})
		} else {
			summary, err = r.pipeline(ctx)
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil // interrupted
			}
			return err
		}
		if summary == "" {
			fmt.Fprintf(cfg.Output, "--- Round %d skipped ---\n\n", n)
			continue
		}
		finishRound(cfg.Output, cfg.Folder, n, summary)
	}
	return nil
}

// round is one numbered round in progress.
type round struct {
	cfg       *Config
	num       int
	logPrefix string // path prefix of the round's logs, e.g. .ralph/round-007
}

// pipeline runs the strategist and executor, then the evaluator and
// archivist if the pattern includes them. It returns the strategist's
// objective, or "" if there was none, in which case no other role runs.
func (r *round) pipeline(ctx context.Context) (string, error) {
	objective, err := r.step(ctx, backend.Strategist, prompt.Round{Folder: r.cfg.AgentFolder})
	if err != nil || objective == "" {
		return "", err
	}
	in := prompt.Round{Folder: r.cfg.AgentFolder, Objective: objective}
	if in.ExecutorResult, err = r.step(ctx, backend.Executor, in); err != nil {
		return "", err
	}
	if r.cfg.Pattern.has(backend.Evaluator) {
		if in.EvaluatorResult, err = r.step(ctx, backend.Evaluator, in); err != nil {
			return "", err
		}
	}
	if r.cfg.Pattern.has(backend.Archivist) {
		if _, err = r.step(ctx, backend.Archivist, in); err != nil {
			return "", err
		}
	}
	return objective, nil
}

// step runs one turn of role with its prompt rendered for in. It saves the
// turn's transcript and final answer to the round logs, prints the turn's
// progress, and returns the final answer.
func (r *round) step(ctx context.Context, role backend.Role, in prompt.Round) (string, error) {
	out := r.cfg.Output
	banner(out, r.num, stages[role])
	if r.cfg.OnStep != nil {
		r.cfg.OnStep(r.num, role)
	}
	agent := r.cfg.Agents[role]
	tmpl := agent.Template
	if tmpl == nil {
		tmpl = prompt.ForRole(string(role))
	}
	p, err := tmpl.Render(in)
	if err != nil {
		return "", fmt.Errorf("%s: %w", role, err)
	}

	turn := backend.Turn{
		Role:    role,
		Folder:  r.cfg.AgentFolder,
		Model:   agent.Model,
		Prompt:  p,
		OnEvent: func(e transcript.Event) { printEvent(out, e) },
	}
	logFile, err := r.create(role, ".jsonl")
	if err != nil {
		fmt.Fprintf(out, "[!] Could not save the %s transcript: %v\n", role, err)
	} else {
		turn.Transcript = logFile
		defer logFile.Close()
	}
	result, err := agent.Backend.Run(ctx, turn)
	if err != nil {
		return "", fmt.Errorf("%s: %w", role, err)
	}
	if err := r.saveResult(role, result); err != nil {
		fmt.Fprintf(out, "[!] Could not save the %s log: %v\n", role, err)
	}
	if result == "" {
		fmt.Fprintf(out, "[!] The %s agent produced no output.\n", role)
	}
	fmt.Fprintln(out)
	return result, nil
}

// create creates the round log for role with the given extension. If it
// fails, it returns a nil file and the error.
func (r *round) create(role backend.Role, ext string) (*os.File, error) {
	path := r.logPrefix + "-" + string(role) + ext
	// An agent may have removed the directory, e.g. with git clean.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return os.Create(path)
}

// saveResult writes role's final answer to the round log whose existence
// marks the turn as finished (see ResumeRound).
func (r *round) saveResult(role backend.Role, result string) error {
	data, err := json.Marshal(struct {
		Result string `json:"result"`
	}{result})
	if err != nil {
		return err
	}
	f, err := r.create(role, ".json")
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// printEvent prints a one-line summary of e, or e's text for messages.
func printEvent(out io.Writer, e transcript.Event) {
	switch e.Kind {
	case transcript.Info:
		fmt.Fprintf(out, "  · %s\n", e.Title)
	case transcript.Message:
		fmt.Fprintf(out, "\n%s\n\n", strings.TrimSpace(e.Text))
	case transcript.ToolCall, transcript.FileChange:
		fmt.Fprintf(out, "  ▸ %s\n", e.Title)
	case transcript.Command:
		fmt.Fprintf(out, "  $ %s\n", e.Title)
	case transcript.ToolResult:
		if e.Failed {
			fmt.Fprintf(out, "    ✗ %s\n", firstLine(e.Text))
		}
	case transcript.Error:
		fmt.Fprintf(out, "  [!] %s\n", firstLine(e.Text))
	case transcript.Result:
		mark := "✓"
		if e.Failed {
			mark = "✗"
		}
		fmt.Fprintf(out, "  %s %s\n", mark, e.Title)
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

func banner(out io.Writer, round int, stage string) {
	fmt.Fprintf(out, "%s\n  Round %d — %s...\n%s\n", separator, round, stage, separator)
}

// finishRound commits the round's changes and reports the outcome. Git
// failures are reported but do not stop the loop.
func finishRound(out io.Writer, folder string, round int, summary string) {
	defer fmt.Fprintln(out)
	if !git.IsRepo(folder) || !git.HasChanges(folder) {
		fmt.Fprintf(out, "--- Round %d complete ---\n", round)
		return
	}
	banner(out, round, "Persisting")
	committed, pushed, err := git.CommitAll(folder, commitMessage(summary))
	switch {
	case err != nil && committed:
		fmt.Fprintf(out, "[!] Push failed: %v\n", err)
		fmt.Fprintf(out, "--- Round %d committed locally ---\n", round)
	case err != nil:
		fmt.Fprintf(out, "[!] Commit failed: %v\n", err)
		fmt.Fprintf(out, "--- Round %d complete; changes left uncommitted ---\n", round)
	case pushed:
		fmt.Fprintf(out, "--- Round %d committed and pushed ---\n", round)
	case committed:
		fmt.Fprintf(out, "--- Round %d committed locally (no upstream configured) ---\n", round)
	default:
		fmt.Fprintf(out, "--- Round %d complete ---\n", round)
	}
}

// commitMessage builds a one-line commit subject from the first line of the
// round's summary, shortened to at most 60 characters.
func commitMessage(summary string) string {
	const fallback = "ralph: update project"
	const maxLen = 60
	line, _, _ := strings.Cut(strings.TrimSpace(summary), "\n")
	line = strings.Trim(strings.Join(strings.Fields(line), " "), "-: ")
	if line == "" {
		return fallback
	}
	if utf8.RuneCountInString(line) > maxLen {
		line = strings.TrimSpace(string([]rune(line)[:maxLen]))
	}
	return "ralph: " + line
}
