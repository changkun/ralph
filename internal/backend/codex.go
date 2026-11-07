package backend

import (
	"context"
)

// Codex runs turns through the OpenAI Codex CLI's non-interactive exec mode.
type Codex struct {
	Env Env // where the CLI runs; nil means Host
}

// Run implements Backend.
//
// On the host, Codex runs in its workspace-write sandbox, so the agent can
// edit files in the folder, and a read-only role, the strategist, runs in
// its read-only sandbox. Codex's sandbox does not work inside a container,
// so in an isolated Env every role runs with danger-full-access and the Env
// enforces read-only access. Strategist sessions are not persisted.
func (c *Codex) Run(ctx context.Context, t Turn) (string, error) {
	env := envOrHost(c.Env)
	// codex exec has no system prompt option, so prepend it to the message.
	message := t.Prompt.User
	if t.Prompt.System != "" {
		message = t.Prompt.System + "\n\n" + t.Prompt.User
	}

	args := []string{"exec", "--json", "--skip-git-repo-check", "--sandbox"}
	switch {
	case env.Isolated():
		args = append(args, "danger-full-access")
	case t.Role.ReadOnly():
		args = append(args, "read-only")
	default:
		args = append(args, "workspace-write")
	}
	if t.Role.ReadOnly() {
		args = append(args, "--ephemeral")
	}
	if t.Model != "" {
		args = append(args, "-m", t.Model)
	}
	args = append(args, "-C", t.Folder, message)
	return stream("codex", env.Command(ctx, t.Role.ReadOnly(), t.Folder, "codex", args...), t)
}
