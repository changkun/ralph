# ralph

Autonomous coding loops for [Claude Code](https://claude.com/claude-code) and [OpenAI Codex](https://github.com/openai/codex). Ralph exposes the four patterns described in [Goalless Agents](https://changkun.de/blog/posts/goalless-agents/):

- `standalone`
- `think+act`
- `think+act+evaluator`
- `think+act+evaluator+archivist`

## How it works

Ralph models four agent roles:

- `Strategist`: proposes exactly one next objective
- `Executor`: implements that objective
- `Evaluator`: verifies the latest round without becoming a planner
- `Archivist`: observes the round and records durable knowledge

Git commit and push are infrastructure, not agent roles. After each round, Ralph stages and commits repository changes automatically. If the current branch has an upstream, Ralph pushes too; otherwise it keeps the commit local.

### `standalone`

```
┌─────────────────────────────┐
│  Standalone                 │
│  Chooses one next step      │
│  and implements it          │
└────────────┬────────────────┘
             │
             └──── loop ────► back to Standalone
```

One agent chooses a concrete next step and implements it directly. No dedicated evaluator or archivist is involved. Its round output is saved as `.ralph/round-001-standalone.json`.

### `think+act`

```
┌─────────────────────────────┐
│  Strategist                 │
│  Proposes one next goal     │
└────────────┬────────────────┘
             │
             ▼
┌─────────────────────────────┐
│  Executor                   │
│  Implements the goal        │
└────────────┬────────────────┘
             │
             └──── loop ────► back to Strategist
```

This is the stripped two-role loop. Each round saves `.ralph/round-XXX-strategist.json` and `.ralph/round-XXX-executor.json`.

### `think+act+evaluator`

```
┌─────────────────────────────┐
│  Strategist                 │
│  Proposes one next goal     │
└────────────┬────────────────┘
             │
             ▼
┌─────────────────────────────┐
│  Executor                   │
│  Implements the goal        │
└────────────┬────────────────┘
             │
             ▼
┌─────────────────────────────┐
│  Evaluator                  │
│  Verifies the round         │
└────────────┬────────────────┘
             │
             └──── loop ────► back to Strategist
```

This adds a verification layer without adding a knowledge role. Each round also saves `.ralph/round-XXX-evaluator.json`.

### `think+act+evaluator+archivist`

```
┌─────────────────────────────┐
│  Strategist                 │
│  Proposes one next goal     │
└────────────┬────────────────┘
             │
             ▼
┌─────────────────────────────┐
│  Executor                   │
│  Implements the goal        │
└────────────┬────────────────┘
             │
             ▼
┌─────────────────────────────┐
│  Evaluator                  │
│  Verifies the round         │
└────────────┬────────────────┘
             │
             ▼
┌─────────────────────────────┐
│  Archivist                  │
│  Records durable knowledge  │
└────────────┬────────────────┘
             │
             └──── loop ────► back to Strategist
```

This is the full four-role pipeline from the article. The archivist is the observer/knowledge role: it updates `CLAUDE.md` or `AGENTS.md` and any relevant project documentation, then Ralph persists the round through git. Each round also saves `.ralph/round-XXX-archivist.json`.

On restart, Ralph resumes from the last completed round for the selected pattern.

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

# Run the standalone pattern
./ralph --pattern standalone /path/to/project

# Add evaluation
./ralph --pattern 'think+act+evaluator' /path/to/project

# Run the full four-role pipeline
./ralph --pattern 'think+act+evaluator+archivist' /path/to/project

# Limit to 5 rounds
./ralph --max-rounds 5 /path/to/project
```

Press `Ctrl-C` to stop at any time.

Legacy aliases `think-act`, `strategist-executor`, and `builder` are still accepted. Older `tester` and `documenter` pattern names also continue to normalize to the new evaluator/archivist names. `pipeline` and `full-pipeline` normalize to `think+act+evaluator+archivist`.

## Configuration

| Flag | Default | Description |
|------|---------|-------------|
| `--max-rounds N` | unlimited | Stop after N rounds |
| `--backend claude\|codex` | `claude` | LLM backend to use |
| `--pattern standalone\|think+act\|think+act+evaluator\|think+act+evaluator+archivist` | `think+act` | Execution pattern to use |

## Project structure

```
internal/
├── backend/     Backend interface, Claude and Codex implementations
├── git/         Git helpers for repo detection and persistence
├── loop/        Pattern loops and resume support
└── prompt/      go:embed templates, one .tmpl per role
```

## License

MIT
