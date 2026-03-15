# ralph

An autonomous "think-act" loop for [Claude Code](https://claude.com/claude-code) and [OpenAI Codex](https://github.com/openai/codex). A thinker proposes goals; a worker implements them; a committer pushes the results and documents decisions. Repeat.

## How it works

```
┌─────────────────────────────┐
│  Thinker                    │
│  Analyzes the project,      │
│  proposes one goal          │
└────────────┬────────────────┘
             │
             ▼
┌─────────────────────────────┐
│  Worker                     │
│  Implements the goal,       │
│  modifies the code          │
└────────────┬────────────────┘
             │
             ▼
┌─────────────────────────────┐
│  Committer                  │
│  Commits, pushes, and       │
│  documents knowledge        │
│  (git repos only)           │
└────────────┬────────────────┘
             │
             └──── loop ────► back to Thinker
```

Each agent's prompt is defined as a Go template in `internal/prompt/templates/`. Full JSON output from each thinker and worker is saved in `.ralph/` (e.g. `round-001-thinker.json`, `round-001-worker.json`). The thinker checks this folder for context on what previous rounds proposed.

If the project folder is a git repository, changes are automatically committed and pushed after each round. The committer also maintains project documentation alongside commits. Non-git folders skip the commit step.

On restart, ralph resumes from the last completed round.

## Requirements

- Go 1.22+
- **Claude backend:** [Claude Code CLI](https://claude.com/claude-code) authenticated via OAuth (`claude auth login`)
- **Codex backend:** [Codex CLI](https://github.com/openai/codex) with `CODEX_API_KEY` set

## Usage

```bash
# Build
go build -o ralph .

# Using Claude Code (default), runs until Ctrl-C
./ralph /path/to/project

# Using OpenAI Codex
./ralph --backend codex /path/to/project

# Limit to 5 rounds
./ralph --max-rounds 5 /path/to/project
```

Press `Ctrl-C` to stop at any time.

## Configuration

| Flag                    | Default     | Description                              |
|-------------------------|-------------|------------------------------------------|
| `--max-rounds N`        | unlimited   | Stop after N rounds                      |
| `--backend claude\|codex`| `claude`    | LLM backend to use                       |

## Project structure

```
internal/
├── backend/     Backend interface, Claude and Codex implementations
├── git/         Git helpers (repo detection, change detection)
├── loop/        Main think-act-commit loop with resume support
└── prompt/      go:embed templates, one .tmpl per agent (system + prompt)
```

## License

MIT
