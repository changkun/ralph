#!/usr/bin/env bash
#
# ralph.sh — Run Claude Code or OpenAI Codex in a "think → act" loop.
#
# Usage:
#   ./ralph.sh [--backend claude|codex] [--max-rounds N] <folder>
#
# Requirements:
#   - jq installed
#   - For claude backend: claude CLI authenticated via OAuth (`claude auth login`)
#   - For codex backend: codex CLI installed and CODEX_API_KEY set
#
# How it works:
#   1. A "thinker" instance looks at the folder and proposes one concrete task.
#   2. A "worker" instance executes that task.
#   3. Loop back to step 1. The thinker sees the updated state and picks the next task.
#   Press Ctrl-C to stop.

set -euo pipefail

# --- Parse arguments ---
BACKEND="claude"
MAX_ROUNDS=0  # 0 = unlimited
while [[ $# -gt 0 ]]; do
    case "$1" in
        --backend)
            BACKEND="$2"
            shift 2
            ;;
        --max-rounds)
            MAX_ROUNDS="$2"
            shift 2
            ;;
        -*)
            echo "Unknown option: $1" >&2
            echo "Usage: $0 [--backend claude|codex] [--max-rounds N] <folder>" >&2
            exit 1
            ;;
        *)
            break
            ;;
    esac
done

if [[ "$BACKEND" != "claude" && "$BACKEND" != "codex" ]]; then
    echo "Error: backend must be 'claude' or 'codex', got '$BACKEND'" >&2
    exit 1
fi

FOLDER="${1:?Usage: $0 [--backend claude|codex] [--max-rounds N] <folder>}"
FOLDER="$(cd "$FOLDER" && pwd)"  # resolve to absolute path

ROUND=0

THINKER_PROMPT='Look at this project and propose exactly ONE goal to achieve next. State the goal clearly and concisely. If the project is empty or has no code yet, decide on your own what to build — pick a concrete, interesting project idea and propose it as your goal. Do NOT ask the user what to build.'

WORKER_SYSTEM='Implement the following task. Make the changes directly and summarize what you changed when done.'

RALPH_DIR="$FOLDER/.ralph"
mkdir -p "$RALPH_DIR"

# --- Backend-specific runners ---

run_claude_thinker() {
    local prompt="$1"
    (cd "$FOLDER" && claude -p \
        --dangerously-skip-permissions \
        --output-format json \
        --max-turns 3 \
        "$prompt")
}

run_claude_worker() {
    local prompt="$1"
    (cd "$FOLDER" && claude -p \
        --dangerously-skip-permissions \
        --output-format json \
        --append-system-prompt "$WORKER_SYSTEM" \
        "$prompt")
}

run_codex_thinker() {
    local prompt="$1"
    local tmpfile
    tmpfile=$(mktemp)
    codex exec \
        --full-auto \
        --ephemeral \
        -C "$FOLDER" \
        -o "$tmpfile" \
        "$prompt" \
        >/dev/null 2>&1 || true
    # Wrap in JSON to match claude format
    local text
    text=$(cat "$tmpfile")
    rm -f "$tmpfile"
    jq -n --arg r "$text" '{result: $r}'
}

run_codex_worker() {
    local prompt="$1"
    local tmpfile
    tmpfile=$(mktemp)
    codex exec \
        --full-auto \
        -C "$FOLDER" \
        -o "$tmpfile" \
        "$prompt" \
        >/dev/null 2>&1 || true
    # Wrap in JSON to match claude format
    local text
    text=$(cat "$tmpfile")
    rm -f "$tmpfile"
    jq -n --arg r "$text" '{result: $r}'
}

run_thinker() {
    case "$BACKEND" in
        claude) run_claude_thinker "$1" ;;
        codex)  run_codex_thinker "$1" ;;
    esac
}

run_worker() {
    case "$BACKEND" in
        claude) run_claude_worker "$1" ;;
        codex)  run_codex_worker "$1" ;;
    esac
}

# --- Cleanup ---

cleanup() {
    echo ""
    echo "=== Ralph loop stopped after $ROUND rounds ==="
    echo "Logs saved to: $RALPH_DIR"
    exit 0
}
trap cleanup INT TERM

# --- Main loop ---

echo "=== Ralph loop starting ==="
echo "Backend: $BACKEND"
echo "Folder: $FOLDER"

if [[ "$MAX_ROUNDS" -gt 0 ]]; then
    echo "Max rounds: $MAX_ROUNDS"
fi
echo "Press Ctrl-C to stop."
echo ""

while [[ "$MAX_ROUNDS" -eq 0 ]] || [[ "$ROUND" -lt "$MAX_ROUNDS" ]]; do
    ROUND=$((ROUND + 1))
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "  Round $ROUND — Thinking..."
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    # Step 1: Thinker — analyze the folder and propose one task
    ROUND_PREFIX="$RALPH_DIR/round-$(printf '%03d' $ROUND)"
    THINKER_RAW=$(run_thinker "$THINKER_PROMPT") || true
    echo "$THINKER_RAW" > "${ROUND_PREFIX}-thinker.json"

    IDEA=$(echo "$THINKER_RAW" | jq -r '.result // empty')

    if [ -z "$IDEA" ]; then
        echo "[!] Thinker produced no output. Retrying in 5s..."
        sleep 5
        continue
    fi

    echo ""
    echo "$IDEA"
    echo ""

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "  Round $ROUND — Working..."
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    # Step 2: Worker — execute the task
    WORKER_PROMPT="$WORKER_SYSTEM

Here is your task to execute in this project ($FOLDER):

$IDEA"
    WORKER_RAW=$(run_worker "$WORKER_PROMPT") || true
    echo "$WORKER_RAW" > "${ROUND_PREFIX}-worker.json"

    RESULT=$(echo "$WORKER_RAW" | jq -r '.result // empty')

    if [ -z "$RESULT" ]; then
        echo "[!] Worker produced no output."
    else
        echo ""
        echo "$RESULT"
        echo ""
    fi

    # Step 3: Auto-commit and push if there are changes (only in git repos)
    if git -C "$FOLDER" rev-parse --is-inside-work-tree >/dev/null 2>&1 \
       && [ -n "$(git -C "$FOLDER" status --porcelain 2>/dev/null)" ]; then
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "  Round $ROUND — Committing..."
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

        COMMIT_PROMPT="Look at the current git diff (staged and unstaged) and untracked files in this repository. Create a commit that includes ALL changes with a clear, descriptive commit message explaining what changed and why. Then push to the remote.

Context from the worker: $RESULT"

        case "$BACKEND" in
            claude)
                (cd "$FOLDER" && claude -p \
                    --dangerously-skip-permissions \
                    --output-format json \
                    --max-turns 5 \
                    --append-system-prompt "You are a git committer. Stage all changes, write a clear commit message with a summary title and description body explaining what was changed, then push." \
                    "$COMMIT_PROMPT") \
                    | jq -r '.result // empty' || true
                ;;
            codex)
                COMMIT_TMPFILE=$(mktemp)
                codex exec \
                    --full-auto \
                    -C "$FOLDER" \
                    -o "$COMMIT_TMPFILE" \
                    "$COMMIT_PROMPT" \
                    >/dev/null 2>&1 || true
                cat "$COMMIT_TMPFILE"
                rm -f "$COMMIT_TMPFILE"
                ;;
        esac

        echo ""
        echo "--- Round $ROUND committed and pushed ---"
    else
        echo "--- Round $ROUND complete ---"
    fi

    echo ""
    sleep 2
done
