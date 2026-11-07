# ralph

Ralph runs [Claude Code](https://claude.com/claude-code) or [OpenAI Codex](https://github.com/openai/codex) in an autonomous loop on a project folder. You give it a folder, not a task: in each round the agents decide what to do next and do it, then Ralph commits the result. Ralph implements the four patterns described in [Goalless Agents](https://changkun.de/blog/posts/goalless-agents/).

Ralph comes as a command-line tool, `ralph`, and a desktop app, `ralph-gui`. Either can run the agents in a fresh container that sees only the project folder, so that nothing else on your machine shapes or is touched by their work.

> [!WARNING]
> Ralph runs agents unattended with write access to the folder. Claude Code runs with `--dangerously-skip-permissions`; Codex runs in its `workspace-write` sandbox on this machine and without a sandbox in a container. Only the strategist is limited to reading. After every round, Ralph commits all changes with `git add -A` and pushes if the current branch has an upstream. Use a branch or a clone you are prepared to let it change, and prefer running agents in a container.

## Roles and patterns

Ralph has four agent roles:

- **Strategist** proposes exactly one next objective. It only reasons, so it can only read the project. In a container, it runs in a second container that mounts the folder read-only. On this machine, Claude Code gets just the Read, Glob, and Grep tools and no MCP servers, and Codex runs in its `read-only` sandbox.
- **Executor** implements that objective.
- **Evaluator** verifies the round without becoming a planner.
- **Archivist** observes the round and records durable knowledge in `AGENTS.md`, which Claude Code and Codex both read, and in the project documentation.

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

Committing and pushing are infrastructure, not an agent role. Ralph commits once all roles in the round have finished, and pushes only if the current branch has an upstream. If the folder is not a git repository, `ralph` creates one when it starts; the desktop app asks you to run `git init` instead. Commits and pushes run on this machine, with your git identity and credentials, never in the container.

## Rounds, logs, and resuming

Ralph keeps two logs per turn in the folder's `.ralph/` directory:

- `round-NNN-<role>.jsonl` is the agent's full transcript, as the CLI streamed it: messages, reasoning, tool calls and their output, and the final result. The desktop app shows these as they are written.
- `round-NNN-<role>.json` holds the agent's final answer. It is written only once the turn finishes, which is how Ralph knows where to resume.

Because Ralph commits with `git add -A`, these logs are committed along with the project unless `.ralph/` is in the project's `.gitignore`. The exception is `.ralph/local/`, described under [The desktop app](#the-desktop-app), which Ralph lists in the repository's `.git/info/exclude` so that it is never committed.

- **Skipped rounds.** If the strategist (or the standalone agent) produces no output, Ralph skips the round and starts the next one. Skipped rounds still count toward `--rounds`.
- **Failures.** If an agent's CLI fails, for example because it is not signed in, Ralph stops and prints the error.
- **Stopping.** Ctrl-C, or Stop in the desktop app, stops the running agent, removes the containers, and exits. Changes from an unfinished round stay uncommitted in the working tree.
- **Resuming.** On start, Ralph continues after the last round of the selected pattern that finished. `--rounds` counts only the rounds of this run: if five rounds have finished, `--rounds 3` runs rounds 6 to 8, and running it again runs 9 to 11.

## Running agents in a container

With `--runtime podman` or `--runtime docker` (the desktop app defaults to the first one installed), Ralph runs every agent in a container instead of on this machine:

- **A clean environment.** The image is Debian with Node.js, git, ripgrep, and the Claude Code and Codex CLIs. Ralph builds it from an embedded [Containerfile](internal/sandbox/Containerfile) the first time it is needed, which takes a few minutes, and tags it `ralph-agent:<hash>` so that a changed Containerfile builds a new image. Agents run as an unprivileged user with `sudo`, so they can install what the project needs.
- **Only the project folder.** The folder is mounted at `/work`, and agents see nothing else of this machine: no home directory, no settings, hooks, or MCP servers of your own CLIs. Files they create belong to you.
- **A read-only strategist.** Each session starts two containers, one mounting the folder writable and one read-only, and the strategist runs in the second.
- **Logins.** Your CLIs' logins on this machine do not carry over, so Ralph passes in what it can find for each backend the session uses:

  | Backend | Logs in with |
  |---|---|
  | Claude Code | `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, or `CLAUDE_CODE_OAUTH_TOKEN`. A subscription login is kept in the macOS Keychain and cannot be passed in; run `claude setup-token` and set `CLAUDE_CODE_OAUTH_TOKEN` to its output instead. |
  | Codex | `CODEX_API_KEY` or `OPENAI_API_KEY`, or a copy of `~/.codex/auth.json` from `codex login`. |

  Environment variables are passed by name, so their values never appear on a command line. Ralph refuses to start a session in which a backend has nothing to log in with.

Containers are named `ralph-<hash of the folder>-rw` and `-ro`, and removed when the session ends; a session also removes any left over from one that did not end cleanly.

On Linux, Podman maps your user to the agents' user, so files they create belong to you. Docker on Linux does so only if your user ID is 1000. On hosts that enforce SELinux, the bind mount may need relabeling, which Ralph does not do.

## The desktop app

`ralph-gui` runs sessions like `ralph` does, with settings for each role and a live view of the agents' work:

1. Choose a project folder, which must be a git repository. If it is not one, the app says so and Start stays disabled until you run `git init` in it.
2. Choose where agents run (Podman, Docker, or this machine) and the pattern.
3. For each role in the pattern, choose Claude Code or Codex and a model, and edit its system prompt and prompt if you like. Prompts are Go templates; Reset restores the built-in ones.
4. Press Start, after setting how many rounds it runs if you like; left empty, the session runs until you stop it. Once rounds have finished, the button reads Continue and runs that many more. The app checks the settings and logins, then runs the session in a background process, so it keeps running if you quit the app; open the folder again to follow it. Stop ends it.
5. Watch each turn: the app lists the rounds and turns, and shows the selected turn's messages, reasoning, tool calls, commands, and results as they happen. With *Follow the session* checked, it moves to each new turn by itself. The engine log shows the session's console output, including the first image build and any error.

The app keeps its files in the project's `.ralph/local/`: the settings (`config.json`, leaving out prompts equal to the built-in ones), the session's state (`status.json`), and the engine log (`engine.log`). Agents in a container cannot see this folder, which is replaced by an empty one in their view, and Ralph never commits it.

> [!NOTE]
> Start the app from a terminal, so that it sees the API keys set in your shell. An app opened from the macOS Finder does not.

## Requirements

- Go 1.25.7 or newer, to build
- git
- To run agents in a container: [Podman](https://podman.io) (the default) or Docker. Nothing else; the image includes the agent CLIs.
- To run agents on this machine:
  - For the Claude backend: the [Claude Code CLI](https://claude.com/claude-code), signed in with `claude auth login`
  - For the Codex backend: the [Codex CLI](https://github.com/openai/codex), signed in with `codex login` or with `CODEX_API_KEY` set
- To build the desktop app on Linux, the [Gio dependencies](https://gioui.org/doc/install/linux) for your distribution

## Usage

```bash
go build -o ralph .
go build -o ralph-gui ./cmd/ralph-gui

# The desktop app, optionally opening a folder
./ralph-gui /path/to/project

# Claude Code with the think+act pattern on this machine, until Ctrl-C
./ralph /path/to/project

# The same, with the agents in a Podman container
./ralph --runtime podman /path/to/project

# OpenAI Codex with a chosen model
./ralph --backend codex --model gpt-5 /path/to/project

# Choose a pattern
./ralph --pattern standalone /path/to/project
./ralph --pattern think+act+evaluator+archivist /path/to/project

# Run five rounds, then stop; run it again to continue
./ralph --rounds 5 /path/to/project
```

| Option | Default | Description |
|---|---|---|
| `--runtime host\|podman\|docker` | `host` | Where agents run: on this machine, or in a container; see [Running agents in a container](#running-agents-in-a-container) |
| `--backend claude\|codex` | `claude` | Agent CLI to run |
| `--model NAME` | the CLI's default | Model for every role |
| `--pattern NAME` | `think+act` | Roles in each round; see [Roles and patterns](#roles-and-patterns) |
| `--rounds N` | `0` (no limit) | Run N rounds after those already finished, then stop |
| `-h`, `--help` | | Show usage |

The command-line tool runs every role with the same backend, model, and built-in prompts. To set them per role, use the desktop app.

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
main.go          The ralph command: argument parsing and startup
cmd/ralph-gui/   The desktop app, built with Gio
internal/
├── backend/     Runs a turn through the Claude Code or Codex CLI, on this machine or in a sandbox
├── git/         Repository setup, and commit and push after each round
├── loop/        Patterns, the round loop, and resuming
├── prompt/      Prompt templates; one embedded .tmpl per role
├── sandbox/     The agent image and its containers
├── session/     Settings, status, and running a session in the background
└── transcript/  Reads the CLIs' event streams for display
```

## License

MIT
