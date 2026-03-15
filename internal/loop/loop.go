package loop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"changkun.de/ralph/internal/backend"
	"changkun.de/ralph/internal/git"
	"changkun.de/ralph/internal/prompt"
)

const separator = "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

// Run executes the think-act-commit loop.
func Run(ctx context.Context, be backend.Backend, folder, ralphDir string, round *int, maxRounds int) {
	for maxRounds == 0 || *round < maxRounds {
		*round++
		prefix := filepath.Join(ralphDir, fmt.Sprintf("round-%03d", *round))

		fmt.Printf("%s\n  Round %d — Thinking...\n%s\n", separator, *round, separator)

		thinkerRaw, err := be.RunThinker(ctx, folder, prompt.ThinkerPromptForRound(*round))
		if err != nil && ctx.Err() != nil {
			return
		}
		_ = os.WriteFile(prefix+"-thinker.json", thinkerRaw, 0o644)

		idea := backend.ExtractResult(thinkerRaw)
		if idea == "" {
			fmt.Println("[!] Thinker produced no output. Retrying...")
			continue
		}

		fmt.Printf("\n%s\n\n%s\n  Round %d — Working...\n%s\n", idea, separator, *round, separator)

		workerRaw, err := be.RunWorker(ctx, folder, prompt.WorkerPrompt(folder, idea))
		if err != nil && ctx.Err() != nil {
			return
		}
		_ = os.WriteFile(prefix+"-worker.json", workerRaw, 0o644)

		result := backend.ExtractResult(workerRaw)
		if result == "" {
			fmt.Println("[!] Worker produced no output.")
		} else {
			fmt.Printf("\n%s\n\n", result)
		}

		if git.IsRepo(folder) && git.HasChanges(folder) {
			fmt.Printf("%s\n  Round %d — Committing...\n%s\n", separator, *round, separator)
			cr, err := be.RunCommitter(ctx, folder, prompt.CommitPrompt(idea, result))
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
