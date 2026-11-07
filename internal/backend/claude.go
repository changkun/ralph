package backend

import (
	"context"
)

// Claude runs turns through the Claude Code CLI in print mode.
type Claude struct {
	Env Env // where the CLI runs; nil means Host
}

// Run implements Backend.
//
// On the host, a read-only role, the strategist, gets only the file-reading
// tools and no MCP servers, whose tools could write; every other role runs
// with --dangerously-skip-permissions, so the agent edits files and runs
// commands without asking. In an isolated Env, every role runs with
// --dangerously-skip-permissions and the Env enforces read-only access.
func (c *Claude) Run(ctx context.Context, t Turn) (string, error) {
	env := envOrHost(c.Env)
	args := []string{"-p", "--output-format", "stream-json", "--verbose"}
	if t.Role.ReadOnly() && !env.Isolated() {
		// --tools takes a variable number of values, so it must not come
		// directly before the prompt, which it would swallow.
		args = append(args, "--tools", "Read,Glob,Grep", "--strict-mcp-config")
	} else {
		args = append(args, "--dangerously-skip-permissions")
	}
	if t.Model != "" {
		args = append(args, "--model", t.Model)
	}
	if t.Prompt.System != "" {
		args = append(args, "--append-system-prompt", t.Prompt.System)
	}
	args = append(args, t.Prompt.User)
	return stream("claude", env.Command(ctx, t.Role.ReadOnly(), t.Folder, "claude", args...), t)
}
