// Command ralph runs Claude Code or OpenAI Codex in an autonomous loop on a
// project folder, committing the project after every round.
//
// Usage:
//
//	ralph [--backend claude|codex] [--pattern NAME] [--max-rounds N] <folder>
//
// Run ralph --help for the list of patterns.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/changkun/ralph/internal/backend"
	"github.com/changkun/ralph/internal/loop"
)

const usage = `Usage: ralph [options] <folder>

Options:
  --backend claude|codex   agent CLI to run (default claude)
  --pattern NAME           roles in each round (default think+act); one of
                           standalone, think+act, think+act+evaluator,
                           think+act+evaluator+archivist
  --max-rounds N           stop after round N; 0 means no limit (default 0)
  -h, --help               show this help
`

// errHelp reports that the user asked for help.
var errHelp = errors.New("help requested")

type config struct {
	backendName string
	pattern     loop.Pattern
	maxRounds   int
	folder      string
}

func parseArgs(args []string) (config, error) {
	cfg := config{backendName: "claude", pattern: loop.ThinkAct}
	for len(args) > 0 {
		arg := args[0]
		args = args[1:]
		if arg == "-h" || arg == "--help" {
			return cfg, errHelp
		}
		if !strings.HasPrefix(arg, "-") {
			if cfg.folder != "" {
				return cfg, fmt.Errorf("unexpected argument %q: only one folder is allowed", arg)
			}
			cfg.folder = arg
			continue
		}
		if arg != "--backend" && arg != "--pattern" && arg != "--max-rounds" {
			return cfg, fmt.Errorf("unknown option %s", arg)
		}
		if len(args) == 0 {
			return cfg, fmt.Errorf("%s requires a value", arg)
		}
		value := args[0]
		args = args[1:]
		switch arg {
		case "--backend":
			if backend.New(value) == nil {
				return cfg, fmt.Errorf("unknown backend %q; want claude or codex", value)
			}
			cfg.backendName = value
		case "--pattern":
			p, err := loop.ParsePattern(value)
			if err != nil {
				return cfg, err
			}
			cfg.pattern = p
		case "--max-rounds":
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 {
				return cfg, fmt.Errorf("--max-rounds must be a non-negative integer, got %q", value)
			}
			cfg.maxRounds = n
		}
	}
	if cfg.folder == "" {
		return cfg, errors.New("folder is required")
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
	if errors.Is(err, errHelp) {
		fmt.Print(usage)
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n\n%s", err, usage)
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
