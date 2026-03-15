package backend

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"

	"changkun.de/ralph/internal/prompt"
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

	cmd := exec.CommandContext(ctx, "codex", args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Run()

	text, err := os.ReadFile(path)
	if err != nil {
		text = nil
	}
	return json.Marshal(Result{Value: string(text)})
}

func (c *Codex) RunThinker(ctx context.Context, folder string, p prompt.Prompt) ([]byte, error) {
	return codexExec(ctx, folder, p, "--ephemeral")
}

func (c *Codex) RunWorker(ctx context.Context, folder string, p prompt.Prompt) ([]byte, error) {
	return codexExec(ctx, folder, p)
}

func (c *Codex) RunCommitter(ctx context.Context, folder string, p prompt.Prompt) (string, error) {
	raw, err := codexExec(ctx, folder, p)
	if err != nil {
		return "", err
	}
	return ExtractResult(raw), nil
}
