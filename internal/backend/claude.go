package backend

import (
	"context"
	"os/exec"

	"github.com/changkun/ralph/internal/prompt"
)

// Claude implements Backend using the Claude Code CLI.
type Claude struct{}

func claudeExec(ctx context.Context, folder string, p prompt.Prompt) ([]byte, error) {
	args := []string{"-p", "--dangerously-skip-permissions", "--output-format", "json"}
	if p.System != "" {
		args = append(args, "--append-system-prompt", p.System)
	}
	args = append(args, p.User)
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = folder
	return cmd.Output()
}

func (c *Claude) RunThinker(ctx context.Context, folder string, p prompt.Prompt) ([]byte, error) {
	return claudeExec(ctx, folder, p)
}

func (c *Claude) RunWorker(ctx context.Context, folder string, p prompt.Prompt) ([]byte, error) {
	return claudeExec(ctx, folder, p)
}

func (c *Claude) RunArchivist(ctx context.Context, folder string, p prompt.Prompt) ([]byte, error) {
	return claudeExec(ctx, folder, p)
}
