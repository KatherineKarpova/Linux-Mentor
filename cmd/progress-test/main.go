package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"linux-mentor/internal/progress"
)

func main() {
	databasePath := flag.String("db", "", "path to the temporary SQLite database")
	flag.Parse()
	if *databasePath == "" {
		fmt.Fprintln(os.Stderr, "a database path is required")
		os.Exit(2)
	}
	if err := run(*databasePath); err != nil {
		fmt.Fprintf(os.Stderr, "Progress test failed: %v\n", err)
		os.Exit(1)
	}
}

func run(databasePath string) error {
	ctx := context.Background()
	store, err := progress.Open(ctx, databasePath)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	const sessionID = "linux-mentor-progress-test"
	if err := store.UpsertSession(ctx, sessionID, now); err != nil {
		store.Close()
		return err
	}

	evaluations := []struct {
		concept string
		passed  bool
		at      time.Time
	}{
		{"clean-success", true, now},
		{"clean-success", true, now.AddDate(0, 0, 14)},
		{"clean-success", true, now.AddDate(0, 0, 42)},
		{"one-miss", false, now},
		{"one-miss", true, now.Add(time.Minute)},
		{"reinforcement", false, now},
		{"reinforcement", false, now.Add(time.Minute)},
		{"reinforcement", false, now.Add(2 * time.Minute)},
		{"reinforcement", false, now.Add(3 * time.Minute)},
		{"reinforcement", true, now.Add(4 * time.Minute)},
		{"unresolved", false, now},
		{"unresolved", false, now.Add(time.Minute)},
		{"unresolved", false, now.Add(2 * time.Minute)},
	}
	for _, evaluation := range evaluations {
		if _, err := store.RecordEvaluation(ctx, evaluation.concept, evaluation.passed,
			"", "", "temporary scoring test", sessionID, evaluation.at); err != nil {
			store.Close()
			return err
		}
	}
	if err := store.Close(); err != nil {
		return err
	}

	store, err = progress.Open(ctx, databasePath)
	if err != nil {
		return fmt.Errorf("reopen SQLite test database: %w", err)
	}
	defer store.Close()

	concepts, err := store.Concepts(ctx)
	if err != nil {
		return err
	}
	if err := verifyConcepts(ctx, store, concepts, now); err != nil {
		return err
	}
	sessions, err := store.Sessions(ctx)
	if err != nil {
		return err
	}
	if len(sessions) != 1 || sessions[0].ID != sessionID {
		return errors.New("test session did not persist")
	}

	fmt.Println("Scoring and SQLite persistence checks passed.")
	for _, item := range concepts {
		fmt.Printf(
			"%-16s stage=%d status=%-10s attempts=%d last-misses(capped at 2)=%d current-misses=%d next-review=%s\n",
			item.Name, item.Stage, item.Status, item.Attempts, item.LastMisses,
			item.CurrentMisses, item.OkForReviewAt.Format("2006-01-02"),
		)
		blocks, err := store.ReviewBlocks(ctx, item.Name)
		if err != nil {
			return err
		}
		for _, block := range blocks {
			state := "open"
			if block.Passed {
				state = "completed"
			}
			fmt.Printf("  review %d: %s, misses=%d, attempts=%d\n",
				block.Number+1, state, block.Misses, block.Attempts)
		}
	}
	fmt.Printf("Reopened database: %s (%d concepts, %d recorded attempts)\n",
		databasePath, len(concepts), len(evaluations))
	return nil
}

func verifyConcepts(ctx context.Context, store *progress.Store, concepts []progress.Concept, now time.Time) error {
	want := map[string]struct {
		stage         int
		status        string
		attempts      int
		lastMisses    int
		currentMisses int
		lastAttemptAt time.Time
		nextReviewAt  time.Time
	}{
		"clean-success": {
			3, progress.StatusMastered, 3, 0, 0,
			now.AddDate(0, 0, 42), now.AddDate(0, 0, 98),
		},
		"one-miss": {
			1, progress.StatusReview, 2, 1, 0,
			now.Add(time.Minute), now.Add(time.Minute).AddDate(0, 0, 2),
		},
		"reinforcement": {
			1, progress.StatusReinforce, 5, 2, 0,
			now.Add(4 * time.Minute), now.Add(4*time.Minute).AddDate(0, 0, 1),
		},
		"unresolved": {
			0, progress.StatusReinforce, 3, 0, 3,
			now.Add(2 * time.Minute), now.Add(2 * time.Minute),
		},
	}
	if len(concepts) != len(want) {
		return fmt.Errorf("stored %d concepts, want %d", len(concepts), len(want))
	}
	for _, item := range concepts {
		expected, ok := want[item.Name]
		if !ok || item.Stage != expected.stage || item.Status != expected.status ||
			item.Attempts != expected.attempts ||
			item.LastMisses != expected.lastMisses || item.CurrentMisses != expected.currentMisses ||
			!item.LastAttemptAt.Equal(expected.lastAttemptAt) ||
			!item.OkForReviewAt.Equal(expected.nextReviewAt) {
			return fmt.Errorf("unexpected persisted score for %q: %+v", item.Name, item)
		}
	}
	expectedBlocks := map[string][]int{
		"clean-success": {0, 0, 0},
		"one-miss":      {1},
		"reinforcement": {4},
		"unresolved":    {3},
	}
	for _, item := range concepts {
		blocks, err := store.ReviewBlocks(ctx, item.Name)
		if err != nil {
			return err
		}
		expected := expectedBlocks[item.Name]
		if len(blocks) != len(expected) {
			return fmt.Errorf("%q has %d review blocks, want %d", item.Name, len(blocks), len(expected))
		}
		for i, block := range blocks {
			if block.Misses != expected[i] {
				return fmt.Errorf("%q review block %d has %d misses, want %d",
					item.Name, i+1, block.Misses, expected[i])
			}
		}
	}
	return nil
}
