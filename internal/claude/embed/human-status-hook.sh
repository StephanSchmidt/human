#!/bin/bash
# human status hook — sends Claude Code state events to the human daemon
INPUT=$(cat)
EVENT=$(echo "$INPUT" | jq -r '.hook_event_name // empty')
SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // empty')
CWD=$(echo "$INPUT" | jq -r '.cwd // empty')
NOTIFICATION_TYPE=$(echo "$INPUT" | jq -r '.notification_type // empty')
human hook-event "$EVENT" "$SESSION_ID" "$CWD" "$NOTIFICATION_TYPE" 2>/dev/null || true
