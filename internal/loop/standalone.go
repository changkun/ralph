package loop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/changkun/ralph/internal/backend"
	"github.com/changkun/ralph/internal/prompt"
)

// RunStandalone executes the standalone loop: a single agent decides,
// implements, and commits in each round.
func RunStandalone(ctx context.Context, be backend.Backend, folder, ralphDir, memoryFile string, round *int, maxRounds int) {
	for maxRounds == 0 || *round < maxRounds {
		*round++
		prefix := filepath.Join(ralphDir, fmt.Sprintf("round-%03d", *round))

		fmt.Printf("%s\n  Round %d — Standalone...\n%s\n", separator, *round, separator)

		raw, err := be.RunWorker(ctx, folder, prompt.StandalonePrompt(folder, memoryFile))
		if err != nil && ctx.Err() != nil {
			return
		}
		os.MkdirAll(ralphDir, 0o755)
		_ = os.WriteFile(prefix+"-standalone.json", raw, 0o644)

		result := backend.ExtractResult(raw)
		if result == "" {
			fmt.Println("[!] Standalone run produced no output. Retrying...")
			continue
		}

		fmt.Printf("\n%s\n", result)
		fmt.Printf("--- Round %d complete ---\n\n", *round)
	}
}
