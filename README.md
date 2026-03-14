# ralph

An autonomous "think-act" loop for [Claude Code](https://claude.com/claude-code) and [OpenAI Codex](https://github.com/openai/codex). A thinker proposes goals; a worker implements them; a committer pushes the results. Repeat.

## How it works

```
┌─────────────────────────────┐
│  Thinker                    │
│  Analyzes the project,      │
│  proposes one goal           │
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
│  Commits and pushes changes │
│  (git repos only)           │
└────────────┬────────────────┘
             │
             └──── loop ────► back to Thinker
```

The thinker checks `.ralph-ideas/` for context on what previous rounds proposed, and saves each new idea there as well.

If the project folder is a git repository, changes are automatically committed and pushed after each round. Non-git folders skip this step.

## Requirements

- [jq](https://jqlang.github.io/jq/)
- **Claude backend:** [Claude Code CLI](https://claude.com/claude-code) authenticated via OAuth (`claude auth login`)
- **Codex backend:** [Codex CLI](https://github.com/openai/codex) with `CODEX_API_KEY` set

## Usage

```bash
# Using Claude Code (default)
./ralph.sh /path/to/project

# Using OpenAI Codex
./ralph.sh --backend codex /path/to/project
```

Press `Ctrl-C` to stop.

## Configuration

| Environment variable    | Default   | Description                              |
|-------------------------|-----------|------------------------------------------|
| `MAX_BUDGET_PER_TURN`   | `1.00`    | Max USD spend per worker turn (Claude)   |

The Claude thinker and committer are each capped at $0.50 per turn.

## License

MIT
