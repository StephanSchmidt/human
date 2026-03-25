#!/bin/bash
# human status hook — writes Claude Code state events for human tui
INPUT=$(cat)
EVENT=$(echo "$INPUT" | jq -r '.hook_event_name // empty')
SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // empty')
CWD=$(echo "$INPUT" | jq -r '.cwd // empty')
TIMESTAMP=$(date -u +%Y-%m-%dT%H:%M:%S.%3NZ)

EVENT_DIR="$HOME/.claude/human-events"
mkdir -p "$EVENT_DIR"
echo "{\"event\":\"$EVENT\",\"session_id\":\"$SESSION_ID\",\"cwd\":\"$CWD\",\"timestamp\":\"$TIMESTAMP\"}" \
  >> "$EVENT_DIR/events.jsonl"

# Trim to last 100 lines to prevent unbounded growth
tail -100 "$EVENT_DIR/events.jsonl" > "$EVENT_DIR/events.jsonl.tmp" 2>/dev/null && \
  mv "$EVENT_DIR/events.jsonl.tmp" "$EVENT_DIR/events.jsonl"
