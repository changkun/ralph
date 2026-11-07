package backend

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"

	"github.com/changkun/ralph/internal/prompt"
)

// Codex runs turns through the OpenAI Codex CLI's non-interactive exec mode.
type Codex struct{}

// Run implements Backend. Codex runs in the workspace-write sandbox, so the
// agent can edit files in folder. Strategist turns are ephemeral: Codex does
// not persist their sessions.
func (*Codex) Run(ctx context.Context, role Role, folder string, p prompt.Prompt) ([]byte, error) {
	// codex exec writes the agent's final message to the file named by -o.
	f, err := os.CreateTemp("", "ralph-*.txt")
	if err != nil {
		return nil, err
	}
	outFile := f.Name()
	f.Close()
	defer os.Remove(outFile)

	// codex exec has no system prompt option, so prepend it to the message.
	message := p.User
	if p.System != "" {
		message = p.System + "\n\n" + p.User
	}

	args := []string{"exec", "--sandbox", "workspace-write"}
	if role == Strategist {
		args = append(args, "--ephemeral")
	}
	args = append(args, "-C", folder, "-o", outFile, message)

	// output collects stdout, which only repeats the final message, and keeps
	// the tail of stderr so runError can report why a run failed.
	if _, err := output(exec.CommandContext(ctx, "codex", args...)); err != nil {
		return nil, runError("codex", err, "")
	}
	text, err := os.ReadFile(outFile)
	if err != nil {
		return nil, err
	}
	return json.Marshal(Result{Value: string(text)})
}

// MemoryFile implements Backend.
func (*Codex) MemoryFile() string { return "AGENTS.md" }
