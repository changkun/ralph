package loop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/changkun/ralph/internal/backend"
	"github.com/changkun/ralph/internal/git"
	"github.com/changkun/ralph/internal/prompt"
)

const separator = "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

// RunStrategistExecutor executes the strategist-executor loop.
func RunStrategistExecutor(ctx context.Context, be backend.Backend, folder, ralphDir, memoryFile string, round *int, maxRounds int) {
	for maxRounds == 0 || *round < maxRounds {
		*round++
		prefix := filepath.Join(ralphDir, fmt.Sprintf("round-%03d", *round))

		fmt.Printf("%s\n  Round %d — Strategizing...\n%s\n", separator, *round, separator)

		strategistRaw, err := be.RunThinker(ctx, folder, prompt.StrategistPrompt())
		if err != nil && ctx.Err() != nil {
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
		if err != nil && ctx.Err() != nil {
			return
		}
		_ = os.WriteFile(prefix+"-executor.json", executorRaw, 0o644)

		result := backend.ExtractResult(executorRaw)
		if result == "" {
			fmt.Println("[!] Executor produced no output.")
		} else {
			fmt.Printf("\n%s\n\n", result)
		}

		if git.IsRepo(folder) && git.HasChanges(folder) {
			fmt.Printf("%s\n  Round %d — Committing...\n%s\n", separator, *round, separator)
			cr, err := be.RunCommitter(ctx, folder, prompt.CommitPrompt(objective, result, memoryFile))
			if err == nil && cr != "" {
				fmt.Println(cr)
			}
			fmt.Printf("\n--- Round %d committed and pushed ---\n", *round)
		} else {
			fmt.Printf("--- Round %d complete ---\n", *round)
		}
		fmt.Println()
	}
}
