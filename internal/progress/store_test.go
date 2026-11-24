package progress

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenCreatesSQLiteSchemaAndLocalPreferences(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "linux-mentor", "progress.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var tableCount int
	err = store.db.QueryRowContext(ctx, `
		SELECT count(*) FROM sqlite_master
		WHERE type = 'table' AND name IN (
			'sessions', 'concepts', 'attempts', 'user_preferences'
		)
	`).Scan(&tableCount)
	if err != nil {
		t.Fatal(err)
	}
	if tableCount != 4 {
		t.Fatalf("found %d core tables, want 4", tableCount)
	}

	preferences, err := store.Preferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if preferences.InteractionStyle != "socratic" ||
		preferences.Tone != "encouraging" ||
		preferences.ResponseLength != "concise" {
		t.Fatalf("unexpected default preferences: %+v", preferences)
	}

	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	preferences.InteractionStyle = "direct when requested"
	preferences.Tone = "calm"
	preferences.ResponseLength = "brief"
	preferences.AdditionalNotes = "Use plain language."
	if err := store.SavePreferences(ctx, preferences, now); err != nil {
		t.Fatal(err)
	}
	reloaded, err := store.Preferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.InteractionStyle != preferences.InteractionStyle ||
		reloaded.Tone != preferences.Tone ||
		reloaded.ResponseLength != preferences.ResponseLength ||
		reloaded.AdditionalNotes != preferences.AdditionalNotes ||
		!reloaded.UpdatedAt.Equal(now) {
		t.Fatalf("saved preferences did not round-trip: %+v", reloaded)
	}
}

func TestSessionAndAttemptHistoryAreAssociatedAndDurable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "progress.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	if err := store.UpsertSession(ctx, "conversation-123", now); err != nil {
		t.Fatal(err)
	}
	if err := store.TouchSession(ctx, "conversation-123", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	item, err := store.RecordEvaluation(
		ctx, "  CHMOD ", false, "incorrect mode", "confused permissions",
		"Mixed up octal digits.", "conversation-123", now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if item.Concept != "chmod" || item.Attempts != 1 || item.CurrentMisses != 1 ||
		item.LastReviewed.IsZero() || !item.NextReview.Equal(now) ||
		item.CommandAccuracy != "incorrect mode" ||
		item.ExplanationDepth != "confused permissions" ||
		item.Notes != "Mixed up octal digits." {
		t.Fatalf("unexpected progress after miss: %+v", item)
	}

	var sessionID sql.NullString
	var missesBefore int
	err = store.db.QueryRowContext(ctx, `
		SELECT session_id, misses_before FROM attempts WHERE concept_name = 'chmod'
	`).Scan(&sessionID, &missesBefore)
	if err != nil {
		t.Fatal(err)
	}
	if !sessionID.Valid || sessionID.String != "conversation-123" || missesBefore != 0 {
		t.Fatalf("attempt session=%+v misses before=%d", sessionID, missesBefore)
	}
	if err := store.EndSession(ctx, "conversation-123", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	sessions, err := reopened.Sessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "conversation-123" ||
		!sessions[0].LastSeenAt.Equal(now.Add(time.Minute)) ||
		sessions[0].EndedAt == nil || !sessions[0].EndedAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("persisted sessions = %+v", sessions)
	}
	concepts, err := reopened.Concepts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(concepts) != 1 || concepts[0].Concept != "chmod" ||
		concepts[0].Attempts != 1 || concepts[0].CurrentMisses != 1 ||
		concepts[0].CommandAccuracy != "incorrect mode" ||
		concepts[0].ExplanationDepth != "confused permissions" ||
		concepts[0].Notes != "Mixed up octal digits." ||
		!concepts[0].LastReviewed.Equal(now) || !concepts[0].NextReview.Equal(now) {
		t.Fatalf("persisted concepts = %+v", concepts)
	}
}

func TestReviewBlocksRetainExactMissesBetweenSuccessfulReviews(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "progress.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		if _, err := store.RecordEvaluation(ctx, "chmod", false, "", "", "", "", now.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	item, err := store.RecordEvaluation(ctx, "chmod", true, "", "", "", "", now.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if item.CurrentMisses != 0 || item.LastMisses != 2 ||
		item.Attempts != 5 || item.Stage != 1 || item.Status != StatusReinforce {
		t.Fatalf("progress after four misses then success = %+v", item)
	}

	// the next review is due one day after the success above
	if _, err := store.RecordEvaluation(ctx, "chmod", true, "", "", "", "", now.Add(4*time.Minute).AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	blocks, err := store.ReviewBlocks(ctx, "chmod")
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 {
		t.Fatalf("got %d blocks, want 2: %+v", len(blocks), blocks)
	}
	if blocks[0].Misses != 4 || !blocks[0].Passed || blocks[0].Attempts != 5 {
		t.Errorf("first review block = %+v, want 4 misses then success", blocks[0])
	}
	if blocks[1].Misses != 0 || !blocks[1].Passed || blocks[1].Attempts != 1 {
		t.Errorf("second review block = %+v, want clean success", blocks[1])
	}
	if item.LastMisses != 2 {
		t.Errorf("last miss bucket = %d, want capped value 2", item.LastMisses)
	}
}

func TestVersionOneDatabaseMigratesWithoutLosingProgress(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "progress.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`
		CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);
		CREATE TABLE sessions (
			id TEXT PRIMARY KEY, started_at TEXT NOT NULL, last_seen_at TEXT NOT NULL, ended_at TEXT
		);
		CREATE TABLE concepts (
			name TEXT PRIMARY KEY,
			stage INTEGER NOT NULL,
			last_misses INTEGER NOT NULL,
			current_misses INTEGER NOT NULL,
			attempts INTEGER NOT NULL,
			status TEXT NOT NULL,
			last_attempt_at TEXT NOT NULL,
			last_success_at TEXT,
			ok_for_review_at TEXT NOT NULL,
			command_accuracy TEXT NOT NULL,
			explanation_depth TEXT NOT NULL,
			notes TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE TABLE attempts (
			id INTEGER PRIMARY KEY,
			concept_name TEXT NOT NULL REFERENCES concepts(name) ON UPDATE CASCADE ON DELETE CASCADE,
			session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL,
			attempted_at TEXT NOT NULL,
			passed INTEGER NOT NULL,
			misses_before INTEGER NOT NULL,
			command_accuracy TEXT NOT NULL,
			explanation_depth TEXT NOT NULL,
			notes TEXT NOT NULL
		);
		CREATE TABLE user_preferences (
			id INTEGER PRIMARY KEY,
			interaction_style TEXT NOT NULL,
			tone TEXT NOT NULL,
			response_length TEXT NOT NULL,
			additional_notes TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		INSERT INTO schema_migrations VALUES (1, '2026-11-20T00:00:00.000000000Z');
		INSERT INTO sessions VALUES (
			'conversation-old', '2026-11-19T00:00:00.000000000Z',
			'2026-11-19T00:00:00.000000000Z', NULL
		);
		INSERT INTO concepts VALUES (
			'chmod', 1, 1, 0, 1, 'review',
			'2026-11-20T00:00:00.000000000Z',
			'2026-11-20T00:00:00.000000000Z',
			'2026-11-22T00:00:00.000000000Z',
			'accurate', 'clear', 'legacy notes',
			'2026-11-20T00:00:00.000000000Z',
			'2026-11-20T00:00:00.000000000Z'
		);
		INSERT INTO attempts VALUES (
			1, 'chmod', 'conversation-old', '2026-11-20T00:00:00.000000000Z',
			1, 1, 'accurate', 'clear', 'legacy notes'
		);
		INSERT INTO user_preferences VALUES (
			1, 'direct', 'calm', 'brief', 'plain words',
			'2026-11-20T00:00:00.000000000Z'
		);
	`)
	if err != nil {
		legacy.Close()
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	concepts, err := store.Concepts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(concepts) != 1 || concepts[0].Attempts != 1 ||
		concepts[0].LastReviewed.Format("2006-01-02") != "2026-11-20" ||
		concepts[0].Notes != "legacy notes" {
		t.Fatalf("migrated concepts = %+v", concepts)
	}
	preferences, err := store.Preferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if preferences.InteractionStyle != "direct" || preferences.AdditionalNotes != "plain words" {
		t.Fatalf("migrated preferences = %+v", preferences)
	}

	columns, err := store.db.QueryContext(ctx, `PRAGMA table_info(concepts)`)
	if err != nil {
		t.Fatal(err)
	}
	defer columns.Close()
	removedColumns := map[string]bool{"last_misses": true, "current_misses": true}
	for columns.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := columns.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if removedColumns[name] {
			t.Errorf("redundant column %q remains after migration", name)
		}
	}
	if err := columns.Err(); err != nil {
		t.Fatal(err)
	}
	var staleIndexCount int
	if err := store.db.QueryRowContext(ctx, `
		SELECT count(*) FROM sqlite_master
		WHERE type = 'index' AND name IN (
			'concepts_review_idx', 'attempts_concept_date_idx', 'attempts_session_idx'
		)
	`).Scan(&staleIndexCount); err != nil {
		t.Fatal(err)
	}
	if staleIndexCount != 0 {
		t.Errorf("found %d redundant indexes after migration", staleIndexCount)
	}
	if err := store.db.QueryRowContext(ctx, `PRAGMA foreign_key_check`).Scan(
		new(sql.NullString), new(sql.NullString), new(sql.NullString), new(sql.NullString),
	); err != sql.ErrNoRows {
		t.Fatalf("foreign key check = %v, want no violations", err)
	}
}

func TestEvaluationWithUnknownSessionCreatesIt(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "progress.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	if _, err := store.RecordEvaluation(
		ctx, "chmod", true, "correct mode", "explained bits", "", "never-seen-session", now,
	); err != nil {
		t.Fatalf("evaluation for a session the window never registered: %v", err)
	}

	var sessions, attempts int
	if err := store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions WHERE id = 'never-seen-session'`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM attempts WHERE session_id = 'never-seen-session'`).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 || attempts != 1 {
		t.Fatalf("sessions=%d attempts=%d, want 1 and 1", sessions, attempts)
	}
}

func openTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "progress.db")
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store, path
}

func answer(t *testing.T, store *Store, concept string, passed bool, at time.Time) ReviewItem {
	t.Helper()
	item, err := store.RecordEvaluation(context.Background(), concept, passed, "", "", "", "", at)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func TestQuickCorrectAnswersDoNotMasterAConcept(t *testing.T) {
	store, _ := openTestStore(t)
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

	answer(t, store, "path", true, now)
	answer(t, store, "path", true, now.Add(time.Minute))
	item := answer(t, store, "path", true, now.Add(2*time.Minute))

	if item.Stage != 1 || item.Status != StatusLearning || item.Attempts != 3 || !item.LastAnswerEarly {
		t.Fatalf("three correct answers in two minutes = %+v, want stage 1 learning with the last answer early", item)
	}
	if want := now.AddDate(0, 0, 14); !item.NextReview.Equal(want) {
		t.Errorf("next review = %v, want %v (unchanged by practice)", item.NextReview, want)
	}
}

func TestCleanReviewsOnSeparateDaysMasterAConcept(t *testing.T) {
	store, _ := openTestStore(t)
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

	answer(t, store, "path", true, now)
	answer(t, store, "path", true, now.AddDate(0, 0, 14))
	item := answer(t, store, "path", true, now.AddDate(0, 0, 14+28))

	if item.Stage != 3 || item.Status != StatusMastered || item.LastAnswerEarly {
		t.Fatalf("three clean on-time reviews = %+v, want mastered", item)
	}
}

func TestEarlyPracticeDoesNotChangeMissHistory(t *testing.T) {
	store, _ := openTestStore(t)
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

	answer(t, store, "path", false, now)
	reviewed := answer(t, store, "path", true, now.Add(time.Minute)) // one miss: due in 2 days
	answer(t, store, "path", true, now.Add(time.Hour))               // practice
	item := answer(t, store, "path", true, reviewed.NextReview)      // clean review, on time

	// the clean review follows a one-miss review, so it restarts at stage 1;
	// if the practice answer had closed a review, this would wrongly be stage 2
	if item.Stage != 1 || item.Status != StatusLearning || item.LastMisses != 0 {
		t.Fatalf("clean review after a one-miss review = %+v, want stage 1 learning", item)
	}
	blocks, err := store.ReviewBlocks(context.Background(), "path")
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 || blocks[0].Misses != 1 || !blocks[0].Passed ||
		blocks[1].Misses != 0 || !blocks[1].Passed || blocks[1].Attempts != 2 {
		t.Fatalf("review blocks = %+v, want [1 miss, passed] then [practice + clean review]", blocks)
	}
}

func TestWrongAnswerBeforeReviewDateStillResets(t *testing.T) {
	store, _ := openTestStore(t)
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

	answer(t, store, "path", true, now)
	answer(t, store, "path", true, now.AddDate(0, 0, 14)) // stage 2
	item := answer(t, store, "path", false, now.AddDate(0, 0, 15))

	if item.Stage != 0 || item.Status != StatusReinforce || item.CurrentMisses != 1 ||
		!item.NextReview.Equal(now.AddDate(0, 0, 15)) {
		t.Fatalf("wrong answer before the review date = %+v, want stage 0 reinforce, due now", item)
	}
}

func TestVersionThreeDatabaseMigratesWithoutLosingProgress(t *testing.T) {
	ctx := context.Background()
	store, path := openTestStore(t)
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	answer(t, store, "path", false, now)
	answer(t, store, "path", true, now.Add(time.Minute))

	// turn it back into a version 3 database: no early column, old view
	if _, err := store.db.ExecContext(ctx, `
		DROP VIEW review_blocks;
		ALTER TABLE attempts DROP COLUMN early;
		CREATE VIEW review_blocks AS SELECT 1 AS placeholder;
		DELETE FROM schema_migrations WHERE version = 4;
		INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES (3, 'x');
	`); err != nil {
		t.Fatal(err)
	}
	store.Close()

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open version 3 database: %v", err)
	}
	defer reopened.Close()
	items, err := reopened.Concepts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Attempts != 2 || items[0].LastMisses != 1 || items[0].Stage != 1 {
		t.Fatalf("progress after migrating = %+v", items)
	}
	var version int
	if err := reopened.db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != currentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, currentSchemaVersion)
	}
}
