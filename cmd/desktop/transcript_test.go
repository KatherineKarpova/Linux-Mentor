package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadTranscriptHandlesPartialLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	reply := `{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","content":"hello","created_at":"2026-11-14T10:00:00Z"}` + "\n"
	user := `{"type":"USER_INPUT","source":"USER_EXPLICIT","status":"DONE","content":"hi","created_at":"2026-11-14T10:00:01Z"}` + "\n"
	half := `{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","content":"wor`
	rest := `ld","created_at":"2026-11-14T10:00:02Z"}` + "\n"

	app := &App{transcriptPath: path}

	// a missing file is not an error
	if err := app.readTranscript(); err != nil {
		t.Fatalf("missing file: %v", err)
	}

	if err := os.WriteFile(path, []byte(reply+user+half), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.readTranscript(); err != nil {
		t.Fatal(err)
	}
	if got := len(app.GetMessages()); got != 1 {
		t.Fatalf("after partial line: %d messages, want 1 (the user record and the half line are skipped)", got)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(rest); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := app.readTranscript(); err != nil {
		t.Fatal(err)
	}
	if got := len(app.GetMessages()); got != 2 {
		t.Fatalf("after completing the line: %d messages, want 2", got)
	}
}
