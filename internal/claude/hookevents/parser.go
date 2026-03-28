package hookevents

import (
	"bufio"
	"bytes"
	"encoding/json"
)

// Parse reads all event lines and returns the latest snapshot per session.
// The file is max 100 lines (trimmed by the hook script), so this is cheap.
func Parse(data []byte) map[string]SessionSnapshot {
	sessions := make(map[string]SessionSnapshot)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var evt Event
		if err := json.Unmarshal(line, &evt); err != nil {
			continue
		}
		if evt.SessionID == "" {
			continue
		}
		snap := sessions[evt.SessionID]
		snap.SessionID = evt.SessionID
		if evt.Cwd != "" {
			snap.Cwd = evt.Cwd
		}
		snap.LastEventAt = evt.Timestamp
		ApplyEvent(&snap, &evt)
		sessions[evt.SessionID] = snap
	}
	return sessions
}

// ApplyEvent updates a session snapshot based on a hook event.
func ApplyEvent(snap *SessionSnapshot, evt *Event) {
	if snap == nil || evt == nil {
		return
	}
	switch evt.EventName {
	case "UserPromptSubmit", "SubagentStart":
		snap.IsWorking = true
		snap.IsBlocked = false
		snap.HasError = false
		snap.IsEnded = false

	case "Stop", "SubagentStop":
		snap.IsWorking = false
		snap.IsBlocked = false
		// HasError intentionally not cleared — Stop after StopFailure keeps error visible

	case "StopFailure":
		snap.IsWorking = false
		snap.IsBlocked = false
		snap.HasError = true

	case "PermissionRequest":
		snap.IsWorking = false
		snap.IsBlocked = true

	case "Notification":
		switch evt.NotificationType {
		case "idle_prompt":
			snap.IsWorking = false
			snap.IsBlocked = false
		case "permission_prompt":
			snap.IsWorking = false
			snap.IsBlocked = true
		}
		// Other notification types are ignored (no state change).

	case "SessionStart":
		snap.IsWorking = false
		snap.IsBlocked = false
		snap.HasError = false
		snap.IsEnded = false

	case "SessionEnd":
		snap.IsWorking = false
		snap.IsBlocked = false
		snap.IsEnded = true
	}
}
