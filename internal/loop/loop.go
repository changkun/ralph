package loop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/changkun/ralph/internal/backend"
	"github.com/changkun/ralph/internal/git"
	"github.com/changkun/ralph/internal/prompt"
)

const separator = "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

// RunThinkAct executes the two-role strategist/executor loop.
func RunThinkAct(ctx context.Context, be backend.Backend, folder, ralphDir string, round *int, maxRounds int) {
	runPipeline(ctx, be, folder, ralphDir, "", round, maxRounds, false, false)
}

// RunThinkActEvaluator executes the strategist/executor/evaluator loop.
func RunThinkActEvaluator(ctx context.Context, be backend.Backend, folder, ralphDir string, round *int, maxRounds int) {
	runPipeline(ctx, be, folder, ralphDir, "", round, maxRounds, true, false)
}

// RunThinkActEvaluatorArchivist executes the full four-role loop.
func RunThinkActEvaluatorArchivist(ctx context.Context, be backend.Backend, folder, ralphDir, memoryFile string, round *int, maxRounds int) {
	runPipeline(ctx, be, folder, ralphDir, memoryFile, round, maxRounds, true, true)
}

func runPipeline(ctx context.Context, be backend.Backend, folder, ralphDir, memoryFile string, round *int, maxRounds int, withEvaluator, withArchivist bool) {
	for maxRounds == 0 || *round < maxRounds {
		*round++
		prefix := filepath.Join(ralphDir, fmt.Sprintf("round-%03d", *round))

		fmt.Printf("%s\n  Round %d — Strategizing...\n%s\n", separator, *round, separator)

		strategistRaw, err := be.RunThinker(ctx, folder, prompt.StrategistPrompt())
		if interrupted(ctx, err) {
			return
		}
		os.MkdirAll(ralphDir, 0o755)
		_ = os.WriteFile(prefix+"-strategist.json", strategistRaw, 0o644)

		objective := backend.ExtractResult(strategistRaw)
		if objective == "" {
			fmt.Println("[!] Strategist produced no output. Retrying...")
			continue
		}

		fmt.Printf("\n%s\n\n%s\n  Round %d — Executing...\n%s\n", objective, separator, *round, separator)

		executorRaw, err := be.RunWorker(ctx, folder, prompt.ExecutorPrompt(folder, objective))
		if interrupted(ctx, err) {
			return
		}
		_ = os.WriteFile(prefix+"-executor.json", executorRaw, 0o644)

		executorResult := backend.ExtractResult(executorRaw)
		printResult("Executor", executorResult)

		evaluatorResult := ""
		if withEvaluator {
			fmt.Printf("%s\n  Round %d — Evaluating...\n%s\n", separator, *round, separator)

			evaluatorRaw, err := be.RunWorker(ctx, folder, prompt.EvaluatorPrompt(folder, objective, executorResult))
			if interrupted(ctx, err) {
				return
			}
			_ = os.WriteFile(prefix+"-evaluator.json", evaluatorRaw, 0o644)

			evaluatorResult = backend.ExtractResult(evaluatorRaw)
			printResult("Evaluator", evaluatorResult)
		}

		if withArchivist {
			fmt.Printf("%s\n  Round %d — Archiving...\n%s\n", separator, *round, separator)

			archivistRaw, err := be.RunArchivist(ctx, folder, prompt.ArchivistPrompt(folder, objective, executorResult, evaluatorResult, memoryFile))
			if interrupted(ctx, err) {
				return
			}
			_ = os.WriteFile(prefix+"-archivist.json", archivistRaw, 0o644)

			archivistResult := backend.ExtractResult(archivistRaw)
			printResult("Archivist", archivistResult)
		}

		finishRound(folder, *round, objective)
	}
}

func printResult(role, result string) {
	if result == "" {
		fmt.Printf("[!] %s produced no output.\n\n", role)
		return
	}
	fmt.Printf("\n%s\n\n", result)
}

func finishRound(folder string, round int, summary string) {
	if git.IsRepo(folder) && git.HasChanges(folder) {
		fmt.Printf("%s\n  Round %d — Persisting...\n%s\n", separator, round, separator)

		committed, pushed, err := git.CommitAll(folder, commitMessage(summary))
		switch {
		case err == nil && pushed:
			fmt.Printf("--- Round %d committed and pushed ---\n", round)
		case err == nil && committed:
			fmt.Printf("--- Round %d committed locally (no upstream configured) ---\n", round)
		case err == nil:
			fmt.Printf("--- Round %d complete ---\n", round)
		case pushed:
			fmt.Printf("[!] Push failed after commit: %v\n", err)
			fmt.Printf("--- Round %d committed locally ---\n", round)
		default:
			fmt.Printf("[!] Git persistence failed: %v\n", err)
			fmt.Printf("--- Round %d complete with local changes still present ---\n", round)
		}
	} else {
		fmt.Printf("--- Round %d complete ---\n", round)
	}
	fmt.Println()
}

func commitMessage(summary string) string {
	line := strings.TrimSpace(summary)
	if line == "" {
		return "ralph: update project"
	}
	line = strings.Split(line, "\n")[0]
	line = strings.Join(strings.Fields(line), " ")
	line = strings.Trim(line, "-: ")
	if line == "" {
		return "ralph: update project"
	}
	const maxLen = 60
	if len(line) > maxLen {
		line = strings.TrimSpace(line[:maxLen])
	}
	return "ralph: " + line
}

func interrupted(ctx context.Context, err error) bool {
	return err != nil && ctx.Err() != nil
}
