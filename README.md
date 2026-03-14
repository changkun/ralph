# ralph

An autonomous "think-act" loop for [Claude Code](https://claude.com/claude-code) and [OpenAI Codex](https://github.com/openai/codex). A thinker analyzes your project and proposes tasks; a worker executes them. Repeat.

## How it works

```
┌─────────────────────────────┐
│  Thinker                    │
│  Reads the folder, proposes │
│  one concrete task          │
└────────────┬────────────────┘
             │
             ▼
┌─────────────────────────────┐
│  Worker                     │
│  Executes the task,         │
│  modifies the code          │
└────────────┬────────────────┘
             │
             └──── loop ────► back to Thinker
```

Each round, the thinker sees what previous rounds already did (via `.ralph-log`) and picks something new.

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

The Claude thinker is capped at $0.50 per turn.

## License

MIT
