package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"linux-mentor/internal/progress"
)

func TestStartupCreatesLocalDatabaseAndTracksConversationSession(t *testing.T) {
	stateHome := t.TempDir()
	transcriptPath := filepath.Join(t.TempDir(), "transcript.jsonl")
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("LINUX_MENTOR_CONVERSATION_ID", "conversation-456")
	t.Setenv("LINUX_MENTOR_TRANSCRIPT_PATH", transcriptPath)

	app := NewApp()
	ctx, cancel := context.WithCancel(context.Background())
	app.startup(ctx)

	sessions, err := app.progressStore.Sessions(context.Background())
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "conversation-456" || sessions[0].EndedAt != nil {
		cancel()
		t.Fatalf("startup sessions = %+v", sessions)
	}
	if _, err := os.Stat(filepath.Join(stateHome, "linux-mentor", "progress.db")); err != nil {
		cancel()
		t.Fatalf("startup did not create local database: %v", err)
	}

	cancel()
	app.shutdown(context.Background())
	reopened, err := progress.OpenDefault(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	sessions, err = reopened.Sessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].EndedAt == nil || sessions[0].EndedAt.Before(time.Now().Add(-time.Minute)) {
		t.Fatalf("shutdown did not persist session end: %+v", sessions)
	}
}

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
