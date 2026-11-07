package backend

import (
	"context"
	"os/exec"

	"github.com/changkun/ralph/internal/prompt"
)

// Claude runs turns through the Claude Code CLI in print mode.
type Claude struct{}

// Run implements Backend. A read-only role, the strategist, gets only the
// file-reading tools and no MCP servers, whose tools could write. Every other
// role runs with --dangerously-skip-permissions, so the agent edits files and
// runs commands without asking. Claude Code's JSON output already has the
// shape Run promises.
func (*Claude) Run(ctx context.Context, role Role, folder string, p prompt.Prompt) ([]byte, error) {
	args := []string{"-p", "--output-format", "json"}
	if role.readOnly() {
		// --tools takes a variable number of values, so it must not come
		// directly before the prompt, which it would swallow.
		args = append(args, "--tools", "Read,Glob,Grep", "--strict-mcp-config")
	} else {
		args = append(args, "--dangerously-skip-permissions")
	}
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
