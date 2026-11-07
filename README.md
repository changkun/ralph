# ralph

Ralph runs [Claude Code](https://claude.com/claude-code) or [OpenAI Codex](https://github.com/openai/codex) in an autonomous loop on a project folder. You give it a folder, not a task: in each round the agents decide what to do next and do it, then Ralph commits the result. Ralph implements the four patterns described in [Goalless Agents](https://changkun.de/blog/posts/goalless-agents/).

> [!WARNING]
> Ralph runs agents unattended with write access to the folder. Claude Code runs with `--dangerously-skip-permissions`, and Codex runs in its `workspace-write` sandbox; only the strategist is limited to reading. After every round, Ralph commits all changes with `git add -A` and pushes if the current branch has an upstream. Use a branch or a clone you are prepared to let it change.

## Roles and patterns

Ralph has four agent roles:

- **Strategist** proposes exactly one next objective. It only reasons, so it can only read the project: Claude Code gets just the Read, Glob, and Grep tools and no MCP servers, and Codex runs in its `read-only` sandbox.
- **Executor** implements that objective.
- **Evaluator** verifies the round without becoming a planner.
- **Archivist** observes the round and records durable knowledge in the backend's memory file (`CLAUDE.md` for Claude Code, `AGENTS.md` for Codex) and in the project documentation.

A pattern selects which roles run in each round:

| Pattern | Each round runs |
|---|---|
| `standalone` | A single agent that chooses the next step and implements it |
| `think+act` (default) | Strategist → Executor |
| `think+act+evaluator` | Strategist → Executor → Evaluator |
| `think+act+evaluator+archivist` | Strategist → Executor → Evaluator → Archivist |

```
┌──────────────┐
│  Strategist  │  proposes one objective
└──────┬───────┘
       ▼
┌──────────────┐
│   Executor   │  implements it
└──────┬───────┘
       ▼
┌──────────────┐
│  Evaluator   │  verifies the round           (think+act+evaluator and up)
└──────┬───────┘
       ▼
┌──────────────┐
│  Archivist   │  records durable knowledge    (think+act+evaluator+archivist)
└──────┬───────┘
       ▼
  git commit, push
       │
       └──────► next round
```

Committing and pushing are infrastructure, not an agent role. Ralph commits once all roles in the round have finished, pushes only if the current branch has an upstream, and skips git entirely if the folder is not a repository.

## Rounds, logs, and resuming

Ralph saves each agent's output in the folder's `.ralph/` directory as `round-NNN-<role>.json`, for example `round-007-executor.json`. Because Ralph commits with `git add -A`, these logs are committed along with the project unless `.ralph/` is in the project's `.gitignore`.

- **Skipped rounds.** If the strategist (or the standalone agent) produces no output, Ralph skips the round and starts the next one. Skipped rounds still count toward `--max-rounds`.
- **Failures.** If an agent's CLI fails, for example because it is not signed in, Ralph stops and prints the error.
- **Stopping.** Ctrl-C stops the running agent and exits. Changes from an unfinished round stay uncommitted in the working tree.
- **Resuming.** On start, Ralph continues after the last round of the selected pattern that finished. `--max-rounds` counts those earlier rounds: if five rounds have finished, `--max-rounds 8` runs three more.

## Requirements

- Go 1.25.7 or newer, to build
- For the Claude backend: the [Claude Code CLI](https://claude.com/claude-code), signed in with `claude auth login`
- For the Codex backend: the [Codex CLI](https://github.com/openai/codex), signed in with `codex login` or with `CODEX_API_KEY` set

## Usage

```bash
go build -o ralph .

# Claude Code with the think+act pattern, until Ctrl-C
./ralph /path/to/project

# OpenAI Codex
./ralph --backend codex /path/to/project

# Choose a pattern
./ralph --pattern standalone /path/to/project
./ralph --pattern think+act+evaluator+archivist /path/to/project

# Stop after round 5
./ralph --max-rounds 5 /path/to/project
```

| Option | Default | Description |
|---|---|---|
| `--backend claude\|codex` | `claude` | Agent CLI to run |
| `--pattern NAME` | `think+act` | Roles in each round; see [Roles and patterns](#roles-and-patterns) |
| `--max-rounds N` | `0` (no limit) | Stop after round N, counting rounds from earlier runs |
| `-h`, `--help` | | Show usage |

### Legacy pattern names

Pattern names from earlier versions still work, and `-` or `_` can replace `+` in any name, as in `think-act` or `think_act`.

| Name | Same as |
|---|---|
| `builder`, `build-only` | `standalone` |
| `strategist+executor` | `think+act` |
| `think+act+tester`, `strategist+executor+evaluator`, `strategist+executor+tester` | `think+act+evaluator` |
| `think+act+tester+documenter`, `strategist+executor+evaluator+archivist`, `pipeline`, `full-pipeline` | `think+act+evaluator+archivist` |

Round logs written under earlier role names (`builder`, `worker`, `tester`, `documenter`) still count when resuming.

## Project structure

```
main.go          Command-line parsing and startup
internal/
├── backend/     Backend interface; Claude Code and Codex implementations
├── git/         Commit and push after each round
├── loop/        Patterns, the round loop, and resuming
└── prompt/      Prompt rendering; one embedded .tmpl per role
```

## License

MIT
