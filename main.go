// Command ralph runs Claude Code or OpenAI Codex in an autonomous loop on a
// project folder, committing the project after every round.
//
// Usage:
//
//	ralph [--runtime host|podman|docker] [--backend claude|codex] [--model NAME]
//	      [--pattern NAME] [--rounds N] <folder>
//
// Run ralph --help for the list of patterns. The ralph-gui command offers
// the same with per-role settings and a live view of the agents' work.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/changkun/ralph/internal/backend"
	"github.com/changkun/ralph/internal/loop"
	"github.com/changkun/ralph/internal/session"
)

const usage = `Usage: ralph [options] <folder>

Runs coding agents in rounds on <folder>, committing it after each round.

Options:
  --runtime host|podman|docker
                           where agents run (default host): on this machine,
                           or in a container that sees only <folder>
  --backend claude|codex   agent CLI to run (default claude)
  --model NAME             model for every role (default: the CLI's default)
  --pattern NAME           roles in each round (default think+act); one of
                           standalone, think+act, think+act+evaluator,
                           think+act+evaluator+archivist
  --rounds N               run N rounds after those already finished, then
                           stop; 0 means no limit (default 0)
  -h, --help               show this help
`

// errHelp reports that the user asked for help.
var errHelp = errors.New("help requested")

type config struct {
	runtime     string
	backendName string
	model       string
	pattern     loop.Pattern
	rounds      int
	folder      string
}

func parseArgs(args []string) (config, error) {
	cfg := config{runtime: session.Host, backendName: "claude", pattern: loop.ThinkAct}
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
		if !slices.Contains([]string{"--runtime", "--backend", "--model", "--pattern", "--rounds"}, arg) {
			return cfg, fmt.Errorf("unknown option %s", arg)
		}
		if len(args) == 0 {
			return cfg, fmt.Errorf("%s requires a value", arg)
		}
		value := args[0]
		args = args[1:]
		switch arg {
		case "--runtime":
			if !slices.Contains(session.Runtimes, value) {
				return cfg, fmt.Errorf("unknown runtime %q; want one of %s", value, strings.Join(session.Runtimes, ", "))
			}
			cfg.runtime = value
		case "--backend":
			if backend.New(value, nil) == nil {
				return cfg, fmt.Errorf("unknown backend %q; want claude or codex", value)
			}
			cfg.backendName = value
		case "--model":
			cfg.model = value
		case "--pattern":
			p, err := loop.ParsePattern(value)
			if err != nil {
				return cfg, err
			}
			cfg.pattern = p
		case "--rounds":
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 {
				return cfg, fmt.Errorf("--rounds must be a non-negative integer, got %q", value)
			}
			cfg.rounds = n
		}
	}
	if cfg.folder == "" {
		return cfg, errors.New("folder is required")
	}
	var err error
	cfg.folder, err = filepath.Abs(cfg.folder)
	return cfg, err
}

func run(ctx context.Context, cfg config) error {
	s := session.Defaults()
	s.Runtime = cfg.runtime
	s.Pattern = cfg.pattern
	s.Rounds = cfg.rounds
	for role, a := range s.Agents {
		a.Backend, a.Model = cfg.backendName, cfg.model
		s.Agents[role] = a
	}
	fmt.Print("Press Ctrl-C to stop.\n")
	return session.Run(ctx, cfg.folder, s, os.Stdout)
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func main() { osExit(realMain(os.Args[1:])) }
