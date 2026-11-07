package backend

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"

	"github.com/changkun/ralph/internal/prompt"
)

// Codex implements Backend using the OpenAI Codex CLI.
type Codex struct{}

func codexExec(ctx context.Context, folder string, p prompt.Prompt, extra ...string) ([]byte, error) {
	f, err := os.CreateTemp("", "ralph-*.txt")
	if err != nil {
		return nil, err
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)

	fullPrompt := p.User
	if p.System != "" {
		fullPrompt = p.System + "\n\n" + p.User
	}

	args := []string{"exec", "--full-auto"}
	args = append(args, extra...)
	args = append(args, "-C", folder, "-o", path, fullPrompt)

	// output collects stdout, which only repeats the final message, and keeps
	// the tail of stderr so runError can report why a run failed.
	if _, err := output(exec.CommandContext(ctx, "codex", args...)); err != nil {
		return nil, runError("codex", err, "")
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return json.Marshal(Result{Value: string(text)})
}

func (c *Codex) RunThinker(ctx context.Context, folder string, p prompt.Prompt) ([]byte, error) {
	return codexExec(ctx, folder, p, "--ephemeral")
}

func (c *Codex) RunWorker(ctx context.Context, folder string, p prompt.Prompt) ([]byte, error) {
	return codexExec(ctx, folder, p)
}

func (c *Codex) RunArchivist(ctx context.Context, folder string, p prompt.Prompt) ([]byte, error) {
	return codexExec(ctx, folder, p)
}
