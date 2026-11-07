// Package loop runs Ralph's rounds. A round runs the roles of a pattern in
// order, then commits the project and pushes it if the branch has an
// upstream.
package loop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/changkun/ralph/internal/backend"
	"github.com/changkun/ralph/internal/git"
	"github.com/changkun/ralph/internal/prompt"
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

// Run runs rounds of pattern p on folder, numbering them from finished+1,
// where finished counts the rounds completed by earlier runs (see
// ResumeRound). Each role's raw output is saved in ralphDir as
// round-NNN-<role>.json.
//
// Run stops after round maxRounds (0 means no limit), when ctx is cancelled,
// or when an agent's CLI fails; only the last case returns an error. A round
// whose strategist or standalone agent produces no output is skipped.
func Run(ctx context.Context, be backend.Backend, p Pattern, folder, ralphDir string, finished, maxRounds int) error {
	for n := finished + 1; maxRounds == 0 || n <= maxRounds; n++ {
		r := round{
			be:        be,
			folder:    folder,
			num:       n,
			logPrefix: filepath.Join(ralphDir, fmt.Sprintf("round-%03d", n)),
		}
		var summary string
		var err error
		if p == Standalone {
			summary, err = r.step(ctx, backend.Standalone, prompt.Standalone(folder))
		} else {
			summary, err = r.pipeline(ctx, p)
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil // interrupted
			}
			return err
		}
		if summary == "" {
			fmt.Printf("--- Round %d skipped ---\n\n", n)
			continue
		}
		finishRound(folder, n, summary)
	}
	return nil
}

// round is one numbered round in progress.
type round struct {
	be        backend.Backend
	folder    string
	num       int
	logPrefix string // path prefix of the round's logs, e.g. .ralph/round-007
}

// pipeline runs the strategist and executor, then the evaluator and
// archivist if p includes them. It returns the strategist's objective, or ""
// if there was none, in which case no other role runs.
func (r *round) pipeline(ctx context.Context, p Pattern) (string, error) {
	objective, err := r.step(ctx, backend.Strategist, prompt.Strategist())
	if err != nil || objective == "" {
		return "", err
	}
	in := prompt.Round{Folder: r.folder, Objective: objective}
	if in.ExecutorResult, err = r.step(ctx, backend.Executor, prompt.Executor(in)); err != nil {
		return "", err
	}
	if p.has(backend.Evaluator) {
		if in.EvaluatorResult, err = r.step(ctx, backend.Evaluator, prompt.Evaluator(in)); err != nil {
			return "", err
		}
	}
	if p.has(backend.Archivist) {
		if _, err = r.step(ctx, backend.Archivist, prompt.Archivist(in)); err != nil {
			return "", err
		}
	}
	return objective, nil
}

// step runs one turn of role, saves its raw output to the round log, and
// prints and returns the agent's final message.
func (r *round) step(ctx context.Context, role backend.Role, p prompt.Prompt) (string, error) {
	banner(r.num, stages[role])
	raw, err := r.be.Run(ctx, role, r.folder, p)
	if err != nil {
		return "", fmt.Errorf("%s: %w", role, err)
	}
	if err := r.save(role, raw); err != nil {
		fmt.Printf("[!] Could not save the %s log: %v\n", role, err)
	}
	result := backend.ExtractResult(raw)
	if result == "" {
		fmt.Printf("[!] The %s agent produced no output.\n\n", role)
	} else {
		fmt.Printf("\n%s\n\n", result)
	}
	return result, nil
}

func (r *round) save(role backend.Role, raw []byte) error {
	path := r.logPrefix + "-" + string(role) + ".json"
	// An agent may have removed the directory, e.g. with git clean.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func banner(round int, stage string) {
	fmt.Printf("%s\n  Round %d — %s...\n%s\n", separator, round, stage, separator)
}

// finishRound commits the round's changes and reports the outcome. Git
// failures are reported but do not stop the loop.
func finishRound(folder string, round int, summary string) {
	defer fmt.Println()
	if !git.IsRepo(folder) || !git.HasChanges(folder) {
		fmt.Printf("--- Round %d complete ---\n", round)
		return
	}
	banner(round, "Persisting")
	committed, pushed, err := git.CommitAll(folder, commitMessage(summary))
	switch {
	case err != nil && committed:
		fmt.Printf("[!] Push failed: %v\n", err)
		fmt.Printf("--- Round %d committed locally ---\n", round)
	case err != nil:
		fmt.Printf("[!] Commit failed: %v\n", err)
		fmt.Printf("--- Round %d complete; changes left uncommitted ---\n", round)
	case pushed:
		fmt.Printf("--- Round %d committed and pushed ---\n", round)
	case committed:
		fmt.Printf("--- Round %d committed locally (no upstream configured) ---\n", round)
	default:
		fmt.Printf("--- Round %d complete ---\n", round)
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
