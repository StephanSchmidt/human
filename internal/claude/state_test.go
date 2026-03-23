package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func makeStateEntry(t *testing.T, typ string, stopReason *string) []byte {
	t.Helper()
	m := map[string]interface{}{
		"type": typ,
	}
	if typ == "assistant" {
		msg := map[string]interface{}{}
		if stopReason != nil {
			msg["stop_reason"] = *stopReason
		}
		m["message"] = msg
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func strPtr(s string) *string { return &s }

func TestDetermineState_EndTurn(t *testing.T) {
	lines := [][]byte{
		makeStateEntry(t, "assistant", strPtr("end_turn")),
	}
	got := DetermineState(lines)
	if got != StateReady {
		t.Errorf("end_turn: got %v, want Ready", got)
	}
}

func TestDetermineState_ToolUse(t *testing.T) {
	lines := [][]byte{
		makeStateEntry(t, "assistant", strPtr("tool_use")),
	}
	got := DetermineState(lines)
	if got != StateBusy {
		t.Errorf("tool_use: got %v, want Busy", got)
	}
}

func TestDetermineState_User(t *testing.T) {
	lines := [][]byte{
		makeStateEntry(t, "user", nil),
	}
	got := DetermineState(lines)
	if got != StateBusy {
		t.Errorf("user: got %v, want Busy", got)
	}
}

func TestDetermineState_NullStopReason(t *testing.T) {
	lines := [][]byte{
		makeStateEntry(t, "assistant", nil),
	}
	got := DetermineState(lines)
	if got != StateBusy {
		t.Errorf("null stop_reason: got %v, want Busy", got)
	}
}

func TestDetermineState_SkipsMetadata(t *testing.T) {
	lines := [][]byte{
		makeStateEntry(t, "assistant", strPtr("end_turn")),
		makeStateEntry(t, "progress", nil),
		makeStateEntry(t, "file-history-snapshot", nil),
	}
	got := DetermineState(lines)
	if got != StateReady {
		t.Errorf("skip metadata: got %v, want Ready", got)
	}
}

func TestDetermineState_Empty(t *testing.T) {
	got := DetermineState(nil)
	if got != StateUnknown {
		t.Errorf("empty: got %v, want Unknown", got)
	}
}

func TestDetermineState_Malformed(t *testing.T) {
	lines := [][]byte{
		[]byte("{invalid json"),
	}
	got := DetermineState(lines)
	if got != StateUnknown {
		t.Errorf("malformed: got %v, want Unknown", got)
	}
}

func TestDetermineState_SkipsMalformedBeforeValid(t *testing.T) {
	lines := [][]byte{
		makeStateEntry(t, "user", nil),
		[]byte("{invalid json"),
	}
	got := DetermineState(lines)
	if got != StateBusy {
		t.Errorf("malformed then user: got %v, want Busy", got)
	}
}

func TestInstanceStateString(t *testing.T) {
	tests := []struct {
		state InstanceState
		want  string
	}{
		{StateUnknown, "⚪"},
		{StateBusy, "🔴"},
		{StateReady, "🟢"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("InstanceState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestOSStateReader(t *testing.T) {
	dir := t.TempDir()

	// Create two JSONL files, make the second one newer.
	old := filepath.Join(dir, "old.jsonl")
	entry := makeStateEntry(t, "assistant", strPtr("end_turn"))
	if err := os.WriteFile(old, append(entry, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	// Set old file to the past.
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}

	newFile := filepath.Join(dir, "new.jsonl")
	busyEntry := makeStateEntry(t, "user", nil)
	if err := os.WriteFile(newFile, append(busyEntry, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	reader := OSStateReader{}
	state, err := reader.ReadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if state != StateBusy {
		t.Errorf("OSStateReader: got %v, want Busy (from newer file)", state)
	}
}

func TestOSStateReader_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	reader := OSStateReader{}
	state, err := reader.ReadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if state != StateUnknown {
		t.Errorf("empty dir: got %v, want Unknown", state)
	}
}

func TestByteStateReader(t *testing.T) {
	entry := makeStateEntry(t, "assistant", strPtr("tool_use"))
	data := append(entry, '\n')
	reader := &ByteStateReader{Data: data}
	state, err := reader.ReadState("")
	if err != nil {
		t.Fatal(err)
	}
	if state != StateBusy {
		t.Errorf("ByteStateReader: got %v, want Busy", state)
	}
}

func TestByteStateReader_Empty(t *testing.T) {
	reader := &ByteStateReader{Data: nil}
	state, err := reader.ReadState("")
	if err != nil {
		t.Fatal(err)
	}
	if state != StateUnknown {
		t.Errorf("empty: got %v, want Unknown", state)
	}
}

func TestFileStateReader_Ready(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	entry := makeStateEntry(t, "assistant", strPtr("end_turn"))
	if err := os.WriteFile(path, append(entry, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	reader := FileStateReader{Path: path}
	state, err := reader.ReadState("")
	if err != nil {
		t.Fatal(err)
	}
	if state != StateReady {
		t.Errorf("FileStateReader ready: got %v, want Ready", state)
	}
}

func TestFileStateReader_Busy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	entry := makeStateEntry(t, "user", nil)
	if err := os.WriteFile(path, append(entry, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	reader := FileStateReader{Path: path}
	state, err := reader.ReadState("")
	if err != nil {
		t.Fatal(err)
	}
	if state != StateBusy {
		t.Errorf("FileStateReader busy: got %v, want Busy", state)
	}
}

func TestReadyStateReader(t *testing.T) {
	reader := ReadyStateReader{}
	state, err := reader.ReadState("")
	if err != nil {
		t.Fatal(err)
	}
	if state != StateReady {
		t.Errorf("ReadyStateReader: got %v, want Ready", state)
	}
}

func TestFileStateReader_MissingFile(t *testing.T) {
	reader := FileStateReader{Path: "/nonexistent/path/session.jsonl"}
	state, err := reader.ReadState("")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if state != StateUnknown {
		t.Errorf("FileStateReader missing: got %v, want Unknown", state)
	}
}
