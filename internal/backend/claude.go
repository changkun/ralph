package backend

import (
	"context"
	"os/exec"

	"github.com/changkun/ralph/internal/prompt"
)

// Claude runs turns through the Claude Code CLI in print mode.
type Claude struct{}

// Run implements Backend. Claude Code runs with --dangerously-skip-permissions,
// so the agent edits files and runs commands without asking. Its JSON output
// already has the shape Run promises.
func (*Claude) Run(ctx context.Context, _ Role, folder string, p prompt.Prompt) ([]byte, error) {
	args := []string{"-p", "--output-format", "json", "--dangerously-skip-permissions"}
	if p.System != "" {
		args = append(args, "--append-system-prompt", p.System)
	}
	args = append(args, p.User)
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = folder
	out, err := output(cmd)
	if err != nil {
		// A failed print-mode run usually still reports its error as the result.
		return out, runError("claude", err, ExtractResult(out))
	}
	return out, nil
}

// MemoryFile implements Backend.
func (*Claude) MemoryFile() string { return "CLAUDE.md" }
