package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadTranscriptShowsCompletedModelResponsesOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	app := &App{transcriptPath: path}

	if err := os.WriteFile(path, []byte(
		"{\"type\":\"USER_INPUT\",\"source\":\"USER_EXPLICIT\",\"status\":\"DONE\",\"content\":\"student question\"}\n"+
			"{\"type\":\"GENERIC\",\"source\":\"MODEL\",\"status\":\"DONE\",\"content\":\"internal content\"}\n"+
			"{\"type\":\"PLANNER_RESPONSE\",\"source\":\"MODEL\",\"status\":\"ERROR\",\"content\":\"failed response\"}\n"+
			"{\"type\":\"PLANNER_RESPONSE\",\"source\":\"MODEL\",\"status\":\"DONE\",\"created_at\":\"2026-01-01T00:00:00Z\",\"content\":\"mentor response\"}\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := app.readTranscript(); err != nil {
		t.Fatal(err)
	}

	messages := app.GetMessages()
	if len(messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(messages))
	}
	if messages[0].Content != "mentor response" {
		t.Fatalf("got message %q, want mentor response", messages[0].Content)
	}
}

func TestReadTranscriptWaitsForCompleteLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	app := &App{transcriptPath: path}
	partial := "{\"type\":\"PLANNER_RESPONSE\",\"source\":\"MODEL\",\"status\":\"DONE\",\"content\":\"hello"

	if err := os.WriteFile(path, []byte(partial), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.readTranscript(); err != nil {
		t.Fatal(err)
	}
	if got := len(app.GetMessages()); got != 0 {
		t.Fatalf("got %d messages before line completion, want 0", got)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(" world\"}\n"); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if err := app.readTranscript(); err != nil {
		t.Fatal(err)
	}
	messages := app.GetMessages()
	if len(messages) != 1 || messages[0].Content != "hello world" {
		t.Fatalf("got messages %#v, want one completed response", messages)
	}
}
