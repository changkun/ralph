// ralph — Run Claude Code or OpenAI Codex autonomously.
//
// Usage:
//
//	ralph [--backend claude|codex] [--pattern standalone|think+act|think+act+evaluator|think+act+evaluator+archivist] [--max-rounds N] <folder>
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/changkun/ralph/internal/backend"
	"github.com/changkun/ralph/internal/loop"
)

type config struct {
	backendName string
	pattern     loop.Pattern
	maxRounds   int
	folder      string
}

func parseArgs(args []string) (config, error) {
	cfg := config{backendName: "claude", pattern: loop.ThinkAct}
	for len(args) > 0 {
		switch args[0] {
		case "--backend":
			if len(args) < 2 {
				return cfg, fmt.Errorf("--backend requires a value")
			}
			cfg.backendName = args[1]
			args = args[2:]
		case "--pattern":
			if len(args) < 2 {
				return cfg, fmt.Errorf("--pattern requires a value")
			}
			p, err := loop.ParsePattern(args[1])
			if err != nil {
				return cfg, err
			}
			cfg.pattern = p
			args = args[2:]
		case "--max-rounds":
			if len(args) < 2 {
				return cfg, fmt.Errorf("--max-rounds requires a value")
			}
			var n int
			if _, err := fmt.Sscanf(args[1], "%d", &n); err != nil || n < 0 {
				return cfg, fmt.Errorf("--max-rounds must be a non-negative integer")
			}
			cfg.maxRounds = n
			args = args[2:]
		default:
			if strings.HasPrefix(args[0], "-") {
				return cfg, fmt.Errorf("unknown option: %s", args[0])
			}
			cfg.folder = args[0]
			args = args[1:]
		}
	}
	if cfg.folder == "" {
		return cfg, fmt.Errorf("folder is required")
	}
	if backend.New(cfg.backendName) == nil {
		return cfg, fmt.Errorf("backend must be 'claude' or 'codex', got '%s'", cfg.backendName)
	}
	var err error
	cfg.folder, err = filepath.Abs(cfg.folder)
	return cfg, err
}

func run(ctx context.Context, cfg config, be backend.Backend) error {
	ralphDir := filepath.Join(cfg.folder, ".ralph")
	if err := os.MkdirAll(ralphDir, 0o755); err != nil {
		return err
	}
	finished := loop.ResumeRound(ralphDir, cfg.pattern)
	fmt.Printf("=== Ralph loop starting ===\nBackend: %s\nPattern: %s\nFolder: %s\n", cfg.backendName, cfg.pattern, cfg.folder)
	if finished > 0 {
		fmt.Printf("Resuming after round %d\n", finished)
	}
	if cfg.maxRounds > 0 {
		fmt.Printf("Max rounds: %d\n", cfg.maxRounds)
	}
	fmt.Print("Press Ctrl-C to stop.\n\n")
	return loop.Run(ctx, be, cfg.pattern, cfg.folder, ralphDir, finished, cfg.maxRounds)
}

// osExit is os.Exit, replaced in tests.
var osExit = os.Exit

func realMain(args []string) int {
	cfg, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\nUsage: ralph [--backend claude|codex] [--pattern NAME] [--max-rounds N] <folder>\n", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, cfg, backend.New(cfg.backendName)); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func main() { osExit(realMain(os.Args[1:])) }
