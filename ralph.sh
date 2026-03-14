#!/usr/bin/env bash
#
# ralph.sh — Run Claude Code or OpenAI Codex in a "think → act" loop.
#
# Usage:
#   ./ralph.sh [--backend claude|codex] <folder>
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
while [[ $# -gt 0 ]]; do
    case "$1" in
        --backend)
            BACKEND="$2"
            shift 2
            ;;
        -*)
            echo "Unknown option: $1" >&2
            echo "Usage: $0 [--backend claude|codex] <folder>" >&2
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

FOLDER="${1:?Usage: $0 [--backend claude|codex] <folder>}"
FOLDER="$(cd "$FOLDER" && pwd)"  # resolve to absolute path

ROUND=0
MAX_BUDGET_PER_TURN="${MAX_BUDGET_PER_TURN:-1.00}"  # dollars, per worker turn (claude only)

THINKER_PROMPT='Look at this project and decide what should be done next. Propose exactly ONE task as a clear objective for another engineer to execute. Be specific about which files to change and what the expected outcome is. Do not repeat previous ideas listed below.'

WORKER_SYSTEM='Implement the following task. Make the changes directly and summarize what you changed when done.'

LOGFILE="$FOLDER/.ralph-log"
touch "$LOGFILE"

# --- Backend-specific runners ---

run_claude_thinker() {
    local prompt="$1"
    claude -p \
        --dangerously-skip-permissions \
        --output-format json \
        --max-turns 3 \
        --max-budget-usd 0.50 \
        "$prompt" \
        2>/dev/null \
        | jq -r '.result // empty'
}

run_claude_worker() {
    local prompt="$1"
    claude -p \
        --dangerously-skip-permissions \
        --output-format json \
        --append-system-prompt "$WORKER_SYSTEM" \
        --max-budget-usd "$MAX_BUDGET_PER_TURN" \
        "$prompt" \
        2>/dev/null \
        | jq -r '.result // empty'
}

run_codex_thinker() {
    local prompt="$1"
    local tmpfile
    tmpfile=$(mktemp)
    codex exec \
        --full-auto \
        --ephemeral \
        -o "$tmpfile" \
        "$prompt" \
        2>/dev/null || true
    cat "$tmpfile"
    rm -f "$tmpfile"
}

run_codex_worker() {
    local prompt="$1"
    local tmpfile
    tmpfile=$(mktemp)
    codex exec \
        --full-auto \
        -o "$tmpfile" \
        "$prompt" \
        2>/dev/null || true
    cat "$tmpfile"
    rm -f "$tmpfile"
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
    echo "Log saved to: $LOGFILE"
    exit 0
}
trap cleanup INT TERM

# --- Main loop ---

echo "=== Ralph loop starting ==="
echo "Backend: $BACKEND"
echo "Folder: $FOLDER"
if [[ "$BACKEND" == "claude" ]]; then
    echo "Budget per worker turn: \$${MAX_BUDGET_PER_TURN}"
fi
echo "Press Ctrl-C to stop."
echo ""

while true; do
    ROUND=$((ROUND + 1))
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "  Round $ROUND — Thinking..."
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    # Build context with previous ideas so the thinker doesn't repeat itself
    PREVIOUS_IDEAS=""
    if [ -s "$LOGFILE" ]; then
        PREVIOUS_IDEAS="

Previous ideas (do NOT repeat these):
$(cat "$LOGFILE")"
    fi

    # Step 1: Thinker — analyze the folder and propose one task
    IDEA=$(run_thinker "${THINKER_PROMPT}${PREVIOUS_IDEAS}") || true

    if [ -z "$IDEA" ]; then
        echo "[!] Thinker produced no output. Retrying in 5s..."
        sleep 5
        continue
    fi

    echo ""
    echo "$IDEA"
    echo ""

    # Log the idea
    TASK_SUMMARY=$(echo "$IDEA" | head -1)
    echo "Round $ROUND: $TASK_SUMMARY" >> "$LOGFILE"

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "  Round $ROUND — Working..."
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    # Step 2: Worker — execute the task
    WORKER_PROMPT="$WORKER_SYSTEM

Here is your task to execute in this project ($FOLDER):

$IDEA"
    RESULT=$(run_worker "$WORKER_PROMPT") || true

    if [ -z "$RESULT" ]; then
        echo "[!] Worker produced no output."
    else
        echo ""
        echo "$RESULT"
        echo ""
    fi

    echo ""
    echo "--- Round $ROUND complete ---"
    echo ""
    sleep 2
done
