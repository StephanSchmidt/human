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
		snap.IsWorking = evt.EventName == "UserPromptSubmit" || evt.EventName == "SubagentStart"
		sessions[evt.SessionID] = snap
	}
	return sessions
}
