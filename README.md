# ralph

Autonomous coding loops for [Claude Code](https://claude.com/claude-code) and [OpenAI Codex](https://github.com/openai/codex). Ralph supports two patterns: `strategist-executor` and `standalone`.

## How it works

### `strategist-executor`

```
┌─────────────────────────────┐
│  Strategist                 │
│  Analyzes the project,      │
│  proposes one goal          │
└────────────┬────────────────┘
             │
             ▼
┌─────────────────────────────┐
│  Executor                   │
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
             └──── loop ────► back to Strategist
```

Each agent's prompt is defined as a Go template in `internal/prompt/templates/`. Full JSON output from each strategist and executor round is saved in `.ralph/` (e.g. `round-001-strategist.json`, `round-001-executor.json`).

### `standalone`

One agent chooses a concrete next step, implements it directly, updates memory if needed, and commits and pushes the result. Its round output is saved as `.ralph/round-001-standalone.json`.

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

# Run the single-agent standalone pattern
./ralph --pattern standalone /path/to/project

# Limit to 5 rounds
./ralph --max-rounds 5 /path/to/project
```

Press `Ctrl-C` to stop at any time.

Legacy pattern aliases `think-act` and `builder` are still accepted and normalize to the new names.

## Configuration

| Flag                                   | Default                 | Description                 |
|----------------------------------------|-------------------------|-----------------------------|
| `--max-rounds N`                       | unlimited               | Stop after N rounds         |
| `--backend claude\|codex`              | `claude`                | LLM backend to use          |
| `--pattern standalone\|strategist-executor` | `strategist-executor` | Execution pattern to use    |

## Project structure

```
internal/
├── backend/     Backend interface, Claude and Codex implementations
├── git/         Git helpers (repo detection, change detection)
├── loop/        Strategist-executor and standalone loops with resume support
└── prompt/      go:embed templates, one .tmpl per agent (system + prompt)
```

## License

MIT
