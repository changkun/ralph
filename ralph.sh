#!/usr/bin/env bash
#
# ralph.sh — Run Claude Code in a "think → act" loop.
#
# Usage:
#   ./ralph.sh <folder>
#
# Requirements:
#   - claude CLI installed and authenticated via OAuth (run `claude auth login` first)
#   - jq installed
#
# How it works:
#   1. A "thinker" Claude looks at the folder and proposes one concrete task.
#   2. A "worker" Claude executes that task.
#   3. Loop back to step 1. The thinker sees the updated state and picks the next task.
#   Press Ctrl-C to stop.

set -euo pipefail

FOLDER="${1:?Usage: $0 <folder>}"
FOLDER="$(cd "$FOLDER" && pwd)"  # resolve to absolute path

ROUND=0
MAX_BUDGET_PER_TURN="${MAX_BUDGET_PER_TURN:-1.00}"  # dollars, per worker turn

THINKER_PROMPT='You are a code analyst. Look at the files in this project folder and think about what could be improved, fixed, or added.

Propose exactly ONE concrete, actionable task. Be specific: name the files, describe the change, and explain why.

Rules:
- Only propose something that can be done with the files in this directory.
- Do NOT propose the same thing twice (check the log below for previous ideas).
- If the project looks complete and well-structured, propose a small but meaningful improvement.
- Keep your response under 300 words.
- Start your response with "TASK:" followed by a one-line summary, then explain the details below.'

WORKER_SYSTEM='You are a software engineer. Execute the task described in the prompt. Make the changes directly — do not ask for confirmation. When done, briefly summarize what you changed.'

LOGFILE="$FOLDER/.ralph-log"
touch "$LOGFILE"

cleanup() {
    echo ""
    echo "=== Ralph loop stopped after $ROUND rounds ==="
    echo "Log saved to: $LOGFILE"
    exit 0
}
trap cleanup INT TERM

echo "=== Ralph loop starting ==="
echo "Folder: $FOLDER"
echo "Budget per worker turn: \$${MAX_BUDGET_PER_TURN}"
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
    IDEA=$(claude -p \
        --dangerously-skip-permissions \
        --output-format json \
        --max-turns 3 \
        --max-budget-usd 0.50 \
        "${THINKER_PROMPT}${PREVIOUS_IDEAS}" \
        2>/dev/null \
        | jq -r '.result // empty') || true

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
    RESULT=$(claude -p \
        --dangerously-skip-permissions \
        --output-format json \
        --append-system-prompt "$WORKER_SYSTEM" \
        --max-budget-usd "$MAX_BUDGET_PER_TURN" \
        "Here is your task to execute in this project ($FOLDER):

$IDEA" \
        2>/dev/null \
        | jq -r '.result // empty') || true

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
