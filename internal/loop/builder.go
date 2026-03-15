package loop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/changkun/ralph/internal/backend"
	"github.com/changkun/ralph/internal/prompt"
)

// RunBuilder executes the builder loop: a single agent that thinks, acts,
// and commits in each round.
func RunBuilder(ctx context.Context, be backend.Backend, folder, ralphDir string, round *int, maxRounds int) {
	for maxRounds == 0 || *round < maxRounds {
		*round++
		prefix := filepath.Join(ralphDir, fmt.Sprintf("round-%03d", *round))

		fmt.Printf("%s\n  Round %d — Building...\n%s\n", separator, *round, separator)

		raw, err := be.RunWorker(ctx, folder, prompt.BuilderPrompt(folder))
		if err != nil && ctx.Err() != nil {
			return
		}
		os.MkdirAll(ralphDir, 0o755)
		_ = os.WriteFile(prefix+"-builder.json", raw, 0o644)

		result := backend.ExtractResult(raw)
		if result == "" {
			fmt.Println("[!] Builder produced no output. Retrying...")
			continue
		}

		fmt.Printf("\n%s\n", result)
		fmt.Printf("--- Round %d complete ---\n\n", *round)
	}
}
