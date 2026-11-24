// Package progress persists learner progress, review history, sessions, and preferences in SQLite.
package progress

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	StatusMastered  = "mastered"
	StatusLearning  = "learning"
	StatusReview    = "review"
	StatusReinforce = "reinforce"

	currentSchemaVersion = 4
	maxStage             = 3
)

const reviewBlocksViewSQL = `
	CREATE VIEW review_blocks AS
	-- A correct answer that counted as a review closes the current block; later
	-- answers start the next one. Early correct answers (practice before the review
	-- date) stay inside the open block and never close it.
	WITH grouped_attempts AS (
		SELECT attempts.*,
			COALESCE(SUM(CASE WHEN passed = 1 AND early = 0 THEN 1 ELSE 0 END) OVER (
				PARTITION BY concept_name ORDER BY id
				ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING
			), 0) AS block_number
		FROM attempts
	)
	SELECT concept_name, block_number,
		MIN(attempted_at) AS started_at,
		MAX(attempted_at) AS last_attempt_at,
		SUM(CASE WHEN passed = 0 THEN 1 ELSE 0 END) AS misses,
		MAX(CASE WHEN passed = 1 AND early = 0 THEN 1 ELSE 0 END) AS passed,
		COUNT(*) AS attempts,
		(SELECT first_attempt.session_id FROM grouped_attempts first_attempt
			WHERE first_attempt.concept_name = grouped_attempts.concept_name
				AND first_attempt.block_number = grouped_attempts.block_number
			ORDER BY first_attempt.id LIMIT 1) AS session_id,
		(SELECT last_attempt.command_accuracy FROM grouped_attempts last_attempt
			WHERE last_attempt.concept_name = grouped_attempts.concept_name
				AND last_attempt.block_number = grouped_attempts.block_number
			ORDER BY last_attempt.id DESC LIMIT 1) AS command_accuracy,
		(SELECT last_attempt.explanation_depth FROM grouped_attempts last_attempt
			WHERE last_attempt.concept_name = grouped_attempts.concept_name
				AND last_attempt.block_number = grouped_attempts.block_number
			ORDER BY last_attempt.id DESC LIMIT 1) AS explanation_depth,
		(SELECT last_attempt.notes FROM grouped_attempts last_attempt
			WHERE last_attempt.concept_name = grouped_attempts.concept_name
				AND last_attempt.block_number = grouped_attempts.block_number
			ORDER BY last_attempt.id DESC LIMIT 1) AS notes
	FROM grouped_attempts
	GROUP BY concept_name, block_number;
`

// Days until the next review, by how many wrong answers came before the correct
// one (row: 0, 1, or 2+) and by stage (column: stage 1, 2, 3).
var intervalDays = [3][maxStage]int{
	{14, 28, 56},
	{2, 4, 8},
	{1, 2, 4},
}

// Concept miss summaries come from review blocks; historical misses are capped at two.
const conceptSelect = `
	SELECT c.name, c.stage,
		COALESCE((SELECT MIN(b.misses, 2) FROM review_blocks b
			WHERE b.concept_name = c.name AND b.passed = 1
			ORDER BY b.block_number DESC LIMIT 1), 0),
		COALESCE((SELECT b.misses FROM review_blocks b
			WHERE b.concept_name = c.name AND b.passed = 0
			ORDER BY b.block_number DESC LIMIT 1), 0),
		(SELECT COUNT(*) FROM attempts a WHERE a.concept_name = c.name),
		c.status,
		COALESCE((SELECT attempted_at FROM attempts a
			WHERE a.concept_name = c.name ORDER BY a.id DESC LIMIT 1), c.created_at),
		c.ok_for_review_at,
		COALESCE((SELECT command_accuracy FROM attempts a
			WHERE a.concept_name = c.name ORDER BY a.id DESC LIMIT 1), ''),
		COALESCE((SELECT explanation_depth FROM attempts a
			WHERE a.concept_name = c.name ORDER BY a.id DESC LIMIT 1), ''),
		COALESCE((SELECT notes FROM attempts a
			WHERE a.concept_name = c.name ORDER BY a.id DESC LIMIT 1), ''),
		COALESCE((SELECT early FROM attempts a
			WHERE a.concept_name = c.name ORDER BY a.id DESC LIMIT 1), 0)
	FROM concepts c
`

type Store struct {
	db *sql.DB
}

// stores learning progress and review scheduling for one linux concept
type ReviewItem struct {
	Concept          string    `json:"concept"`
	Stage            int       `json:"stage"`       // tracks the interval out of 3 for the status category
	LastMisses       int       `json:"last_misses"` // wrong answers before the last correct answer, capped at 2 (0 = clean)
	LastReviewed     time.Time `json:"last_reviewed"`
	NextReview       time.Time `json:"next_review"` // the concept is eligible to come up again after this time
	CommandAccuracy  string    `json:"command_accuracy,omitempty"`
	ExplanationDepth string    `json:"explanation_depth,omitempty"`
	Notes            string    `json:"notes,omitempty"`
	Status           string    `json:"status"`
	Attempts         int       `json:"attempts"`          // total graded answers for this concept
	CurrentMisses    int       `json:"current_misses"`    // wrong answers since the last correct answer
	LastAnswerEarly  bool      `json:"last_answer_early"` // the newest answer was correct but came before the review date
}

type Preferences struct {
	InteractionStyle string
	Tone             string
	ResponseLength   string
	AdditionalNotes  string
	UpdatedAt        time.Time
}

type Session struct {
	ID         string
	StartedAt  time.Time
	LastSeenAt time.Time
	EndedAt    *time.Time
}

type ReviewBlock struct {
	Concept          string
	Number           int
	StartedAt        time.Time
	LastAttemptAt    time.Time
	Misses           int
	Passed           bool
	Attempts         int
	SessionID        string
	CommandAccuracy  string
	ExplanationDepth string
	Notes            string
}

func DefaultPath() (string, error) {
	stateDir := os.Getenv("XDG_STATE_HOME")
	if stateDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home directory: %w", err)
		}
		stateDir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateDir, "linux-mentor", "progress.db"), nil
}

func OpenDefault(ctx context.Context) (*Store, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return Open(ctx, path)
}

func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("database path is empty")
	}
	if path != ":memory:" {
		absPath, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve database path: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(absPath), 0o700); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
		if err := os.Chmod(filepath.Dir(absPath), 0o700); err != nil {
			return nil, fmt.Errorf("secure database directory permissions: %w", err)
		}
		path = absPath
	}

	dsn := path
	if path != ":memory:" {
		fileURL := (&url.URL{Scheme: "file", Path: path}).String()
		dsn = fileURL + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}
	db.SetMaxOpenConns(1)

	store := &Store{db: db}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to SQLite database: %w", err)
	}
	if err := store.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if path != ":memory:" {
		if err := os.Chmod(path, 0o600); err != nil {
			db.Close()
			return nil, fmt.Errorf("secure SQLite database permissions: %w", err)
		}
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) (resultErr error) {
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("prepare SQLite schema migration: %w", err)
	}
	defer func() {
		if _, err := s.db.ExecContext(context.Background(), `PRAGMA foreign_keys = ON`); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("restore SQLite foreign-key enforcement: %w", err)
		}
	}()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin SQLite schema migration: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create SQLite migration table: %w", err)
	}
	var version int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version); err != nil {
		return fmt.Errorf("read SQLite schema version: %w", err)
	}

	switch version {
	case 0:
		if err := createCurrentSchema(ctx, tx); err != nil {
			return err
		}
	case 1, 2, 3:
		// each step upgrades one version; older databases run every later step too
		if version <= 1 {
			if err := migrateSchemaV1(ctx, tx); err != nil {
				return err
			}
		}
		if version <= 2 {
			if err := migrateSchemaV2(ctx, tx); err != nil {
				return err
			}
		}
		if err := migrateSchemaV3(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DROP VIEW IF EXISTS review_blocks;`+reviewBlocksViewSQL); err != nil {
			return fmt.Errorf("rebuild SQLite review blocks view: %w", err)
		}
	case currentSchemaVersion:
	default:
		return fmt.Errorf("unsupported SQLite schema version %d", version)
	}

	now := formatTime(time.Now())
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO user_preferences(id, updated_at) VALUES (1, ?)
	`, now); err != nil {
		return fmt.Errorf("initialize user preferences: %w", err)
	}
	if version < currentSchemaVersion {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)
		`, currentSchemaVersion, now); err != nil {
			return fmt.Errorf("record SQLite schema migration: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit SQLite schema migration: %w", err)
	}
	return nil
}

func createCurrentSchema(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE sessions (
			id TEXT PRIMARY KEY,
			started_at TEXT NOT NULL,
			last_seen_at TEXT NOT NULL,
			ended_at TEXT
		);
		CREATE TABLE concepts (
			name TEXT PRIMARY KEY,
			stage INTEGER NOT NULL DEFAULT 0 CHECK(stage BETWEEN 0 AND 3),
			status TEXT NOT NULL DEFAULT 'learning',
			ok_for_review_at TEXT NOT NULL,
			created_at TEXT NOT NULL
		);
		CREATE TABLE attempts (
			id INTEGER PRIMARY KEY,
			concept_name TEXT NOT NULL REFERENCES concepts(name) ON UPDATE CASCADE ON DELETE CASCADE,
			session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL,
			attempted_at TEXT NOT NULL,
			passed INTEGER NOT NULL CHECK(passed IN (0, 1)),
			misses_before INTEGER NOT NULL CHECK(misses_before >= 0),
			command_accuracy TEXT NOT NULL DEFAULT '',
			explanation_depth TEXT NOT NULL DEFAULT '',
			notes TEXT NOT NULL DEFAULT '',
			early INTEGER NOT NULL DEFAULT 0 CHECK(early IN (0, 1))
		);
		CREATE INDEX attempts_concept_id_idx ON attempts(concept_name, id DESC);
		CREATE TABLE user_preferences (
			id INTEGER PRIMARY KEY CHECK(id = 1),
			interaction_style TEXT NOT NULL DEFAULT 'socratic',
			tone TEXT NOT NULL DEFAULT 'encouraging',
			response_length TEXT NOT NULL DEFAULT 'concise',
			additional_notes TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL
		);
	`+reviewBlocksViewSQL+`
	`); err != nil {
		return fmt.Errorf("create SQLite schema: %w", err)
	}
	return nil
}

// version 3 -> 4: mark correct answers given before the review date
func migrateSchemaV3(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `
		DROP VIEW IF EXISTS review_blocks;
		ALTER TABLE attempts ADD COLUMN early INTEGER NOT NULL DEFAULT 0 CHECK(early IN (0, 1));
	`); err != nil {
		return fmt.Errorf("migrate SQLite early-answer schema: %w", err)
	}
	return nil
}

func migrateSchemaV2(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE concepts_v3 (
			name TEXT PRIMARY KEY,
			stage INTEGER NOT NULL DEFAULT 0 CHECK(stage BETWEEN 0 AND 3),
			status TEXT NOT NULL DEFAULT 'learning',
			ok_for_review_at TEXT NOT NULL,
			created_at TEXT NOT NULL
		);
		INSERT INTO concepts_v3(name, stage, status, ok_for_review_at, created_at)
		SELECT name, stage, status, ok_for_review_at, created_at FROM concepts;
		DROP TABLE concepts;
		ALTER TABLE concepts_v3 RENAME TO concepts;
		DROP INDEX IF EXISTS concepts_review_idx;
	`); err != nil {
		return fmt.Errorf("migrate SQLite review block schema: %w", err)
	}
	return nil
}

func migrateSchemaV1(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE concepts_v2 (
			name TEXT PRIMARY KEY,
			stage INTEGER NOT NULL DEFAULT 0 CHECK(stage BETWEEN 0 AND 3),
			last_misses INTEGER NOT NULL DEFAULT 0 CHECK(last_misses BETWEEN 0 AND 2),
			current_misses INTEGER NOT NULL DEFAULT 0 CHECK(current_misses >= 0),
			status TEXT NOT NULL DEFAULT 'learning',
			ok_for_review_at TEXT NOT NULL,
			created_at TEXT NOT NULL
		);
		INSERT INTO concepts_v2(
			name, stage, last_misses, current_misses, status, ok_for_review_at, created_at
		)
		SELECT name, stage, last_misses, current_misses, status, ok_for_review_at, created_at
		FROM concepts;
		DROP TABLE concepts;
		ALTER TABLE concepts_v2 RENAME TO concepts;
		DROP INDEX IF EXISTS concepts_review_idx;
		DROP INDEX IF EXISTS attempts_concept_date_idx;
		DROP INDEX IF EXISTS attempts_session_idx;
		CREATE INDEX attempts_concept_id_idx ON attempts(concept_name, id DESC);
	`); err != nil {
		return fmt.Errorf("migrate SQLite concepts schema: %w", err)
	}
	return nil
}

func (s *Store) UpsertSession(ctx context.Context, id string, now time.Time) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("session id is empty")
	}
	stamp := formatTime(now)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions(id, started_at, last_seen_at)
		VALUES (?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			last_seen_at = MAX(sessions.last_seen_at, excluded.last_seen_at),
			ended_at = NULL
	`, id, stamp, stamp)
	if err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	return nil
}

func (s *Store) TouchSession(ctx context.Context, id string, now time.Time) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET last_seen_at = MAX(last_seen_at, ?) WHERE id = ? AND ended_at IS NULL
	`, formatTime(now), id)
	if err != nil {
		return fmt.Errorf("update session activity: %w", err)
	}
	return nil
}

func (s *Store) EndSession(ctx context.Context, id string, now time.Time) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET ended_at = ? WHERE id = ?
	`, formatTime(now), id)
	if err != nil {
		return fmt.Errorf("end session: %w", err)
	}
	return nil
}

func (s *Store) Sessions(ctx context.Context) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, started_at, last_seen_at, ended_at
		FROM sessions ORDER BY started_at DESC, id
	`)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var session Session
		var startedAt, lastSeenAt, endedAt sql.NullString
		if err := rows.Scan(&session.ID, &startedAt, &lastSeenAt, &endedAt); err != nil {
			return nil, fmt.Errorf("read session: %w", err)
		}
		if session.StartedAt, err = parseNullTime(startedAt); err != nil {
			return nil, fmt.Errorf("parse session start time: %w", err)
		}
		if session.LastSeenAt, err = parseNullTime(lastSeenAt); err != nil {
			return nil, fmt.Errorf("parse session activity time: %w", err)
		}
		if endedAt.Valid {
			value, err := parseTime(endedAt.String)
			if err != nil {
				return nil, fmt.Errorf("parse session end time: %w", err)
			}
			session.EndedAt = &value
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}
	return sessions, nil
}

// RecordEvaluation saves one graded answer and updates the concept's schedule.
//
// The rule (spaced repetition):
//   - Every answer is saved in the history.
//   - A wrong answer always counts: the concept drops to stage 0, is marked
//     "reinforce", and is due again right away.
//   - A correct answer counts as a review only when the concept is due (its first
//     answer, right after a miss, or on/after its review date). A correct answer
//     before the review date is practice: it is saved with early=1 but changes
//     nothing, so a concept can't be mastered by answering it 3 times in a row.
//   - A counted correct answer closes a review. How many wrong answers came before
//     it (0, 1, or 2+) picks the interval row; repeating the same row as the
//     previous review moves to the next stage (1 -> 2 -> 3), a different row
//     restarts at stage 1. See intervalDays.
//   - Mastered = stage 3 with no wrong answers: three clean reviews in a row, each
//     on a later day (14, then 28, then 56 days apart).
func (s *Store) RecordEvaluation(
	ctx context.Context,
	name string,
	passed bool,
	commandAccuracy string,
	explanationDepth string,
	notes string,
	sessionID string,
	now time.Time,
) (ReviewItem, error) {
	name = NormalizeConcept(name)
	stamp := formatTime(now)
	sessionID = strings.TrimSpace(sessionID)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ReviewItem{}, fmt.Errorf("begin evaluation transaction: %w", err)
	}
	defer tx.Rollback()

	item, err := getConcept(ctx, tx, name)
	if errors.Is(err, sql.ErrNoRows) {
		item = ReviewItem{Concept: name, Status: StatusLearning, NextReview: now}
	} else if err != nil {
		return ReviewItem{}, fmt.Errorf("load concept %q: %w", name, err)
	}

	// a correct answer before the review date is practice, not a review
	early := passed && now.Before(item.NextReview)

	if early {
		// keep the current stage, status, and review date
	} else if passed {
		bucket := missBucket(item.CurrentMisses)
		if item.Stage >= 1 && bucket == missBucket(item.LastMisses) {
			if item.Stage < maxStage {
				item.Stage++
			}
		} else {
			item.Stage = 1
		}
		item.Status = statusFor(bucket, item.Stage)
		item.NextReview = now.AddDate(0, 0, intervalDays[bucket][item.Stage-1])
	} else {
		item.Stage = 0
		item.Status = StatusReinforce
		item.NextReview = now
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO concepts(name, stage, status, ok_for_review_at, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			stage = excluded.stage,
			status = excluded.status,
			ok_for_review_at = excluded.ok_for_review_at
	`, item.Concept, item.Stage, item.Status, formatTime(item.NextReview), stamp)
	if err != nil {
		return ReviewItem{}, fmt.Errorf("update concept %q: %w", name, err)
	}

	// the session row may not exist yet: only the desktop window creates it, and the
	// window is not always started by the session hook (run-mentor, no display).
	var attemptSessionID any
	if sessionID != "" {
		attemptSessionID = sessionID
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO sessions(id, started_at, last_seen_at)
			VALUES (?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				last_seen_at = MAX(sessions.last_seen_at, excluded.last_seen_at)
			WHERE sessions.ended_at IS NULL
		`, sessionID, stamp, stamp); err != nil {
			return ReviewItem{}, fmt.Errorf("save evaluation session %q: %w", sessionID, err)
		}
	}
	var passedValue, earlyValue int
	if passed {
		passedValue = 1
	}
	if early {
		earlyValue = 1
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO attempts(
			concept_name, session_id, attempted_at, passed, misses_before,
			command_accuracy, explanation_depth, notes, early
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.Concept, attemptSessionID, stamp, passedValue, item.CurrentMisses,
		commandAccuracy, explanationDepth, notes, earlyValue)
	if err != nil {
		return ReviewItem{}, fmt.Errorf("save attempt for %q: %w", name, err)
	}
	item, err = getConcept(ctx, tx, name)
	if err != nil {
		return ReviewItem{}, fmt.Errorf("reload concept %q: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return ReviewItem{}, fmt.Errorf("commit evaluation for %q: %w", name, err)
	}
	return item, nil
}

func (s *Store) ReviewBlocks(ctx context.Context, concept string) ([]ReviewBlock, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT concept_name, block_number, started_at, last_attempt_at, misses,
			passed, attempts, session_id, command_accuracy, explanation_depth, notes
		FROM review_blocks WHERE concept_name = ? ORDER BY block_number
	`, NormalizeConcept(concept))
	if err != nil {
		return nil, fmt.Errorf("query review blocks: %w", err)
	}
	defer rows.Close()

	var blocks []ReviewBlock
	for rows.Next() {
		var block ReviewBlock
		var startedAt, lastAttemptAt string
		var passed int
		var sessionID sql.NullString
		if err := rows.Scan(
			&block.Concept, &block.Number, &startedAt, &lastAttemptAt, &block.Misses,
			&passed, &block.Attempts, &sessionID, &block.CommandAccuracy,
			&block.ExplanationDepth, &block.Notes,
		); err != nil {
			return nil, fmt.Errorf("read review block: %w", err)
		}
		if block.StartedAt, err = parseTime(startedAt); err != nil {
			return nil, fmt.Errorf("parse review block start time: %w", err)
		}
		if block.LastAttemptAt, err = parseTime(lastAttemptAt); err != nil {
			return nil, fmt.Errorf("parse review block attempt time: %w", err)
		}
		block.Passed = passed == 1
		block.SessionID = sessionID.String
		blocks = append(blocks, block)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate review blocks: %w", err)
	}
	return blocks, nil
}

func (s *Store) Concepts(ctx context.Context) ([]ReviewItem, error) {
	rows, err := s.db.QueryContext(ctx, conceptSelect+" ORDER BY c.name")
	if err != nil {
		return nil, fmt.Errorf("query concepts: %w", err)
	}
	defer rows.Close()

	var concepts []ReviewItem
	for rows.Next() {
		item, err := scanConcept(rows)
		if err != nil {
			return nil, fmt.Errorf("read concept: %w", err)
		}
		concepts = append(concepts, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate concepts: %w", err)
	}
	return concepts, nil
}

func (s *Store) SavePreferences(ctx context.Context, preferences Preferences, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_preferences(
			id, interaction_style, tone, response_length, additional_notes, updated_at
		) VALUES (1, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			interaction_style = excluded.interaction_style,
			tone = excluded.tone,
			response_length = excluded.response_length,
			additional_notes = excluded.additional_notes,
			updated_at = excluded.updated_at
	`, preferences.InteractionStyle, preferences.Tone, preferences.ResponseLength,
		preferences.AdditionalNotes, formatTime(now))
	if err != nil {
		return fmt.Errorf("save user preferences: %w", err)
	}
	return nil
}

func (s *Store) Preferences(ctx context.Context) (Preferences, error) {
	var preferences Preferences
	var updatedAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT interaction_style, tone, response_length, additional_notes, updated_at
		FROM user_preferences WHERE id = 1
	`).Scan(&preferences.InteractionStyle, &preferences.Tone, &preferences.ResponseLength,
		&preferences.AdditionalNotes, &updatedAt)
	if err != nil {
		return Preferences{}, fmt.Errorf("load user preferences: %w", err)
	}
	preferences.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return Preferences{}, fmt.Errorf("parse preferences update time: %w", err)
	}
	return preferences, nil
}

func NormalizeConcept(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "general-linux"
	}
	return name
}

func ShouldReview(item ReviewItem, now time.Time) bool {
	return !now.Before(item.NextReview)
}

func statusFor(bucket, stage int) string {
	switch bucket {
	case 0:
		if stage >= maxStage {
			return StatusMastered
		}
		return StatusLearning
	case 1:
		return StatusReview
	default:
		return StatusReinforce
	}
}

func missBucket(misses int) int {
	if misses > 2 {
		return 2
	}
	return misses
}

func getConcept(ctx context.Context, tx *sql.Tx, name string) (ReviewItem, error) {
	return scanConcept(tx.QueryRowContext(ctx, conceptSelect+" WHERE c.name = ?", name))
}

type scanner interface {
	Scan(dest ...any) error
}

func scanConcept(row scanner) (ReviewItem, error) {
	var item ReviewItem
	var lastAttempt, reviewAt sql.NullString
	if err := row.Scan(
		&item.Concept, &item.Stage, &item.LastMisses, &item.CurrentMisses, &item.Attempts, &item.Status,
		&lastAttempt, &reviewAt, &item.CommandAccuracy, &item.ExplanationDepth, &item.Notes,
		&item.LastAnswerEarly,
	); err != nil {
		return ReviewItem{}, err
	}
	var err error
	if item.LastReviewed, err = parseNullTime(lastAttempt); err != nil {
		return ReviewItem{}, err
	}
	if item.NextReview, err = parseNullTime(reviewAt); err != nil {
		return ReviewItem{}, err
	}
	return item, nil
}

func parseNullTime(value sql.NullString) (time.Time, error) {
	if !value.Valid {
		return time.Time{}, nil
	}
	return parseTime(value.String)
}

func parseTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}

func formatTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z07:00")
}
