package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"linux-mentor/internal/docs"
	"linux-mentor/internal/progress"
)

func newTestStore(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := initProgressStore(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if store != nil {
			if err := store.Close(); err != nil {
				t.Errorf("close progress store: %v", err)
			}
		}
	})
}

func record(t *testing.T, concept string, passed bool) recordEvaluationOutput {
	t.Helper()
	_, out, err := recordEvaluation(context.Background(), nil, recordEvaluationInput{
		Concept: concept,
		Passed:  passed,
	})
	if err != nil || !out.Success {
		t.Fatalf("recordEvaluation(%q, %v): success=%v err=%v", concept, passed, out.Success, err)
	}
	return out
}

func TestRecordEvaluationNormalizesNamesAndPersistsAttemptHistory(t *testing.T) {
	newTestStore(t)
	record(t, "  CHMOD ", true)
	record(t, "chmod", false)

	concepts, err := store.Concepts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(concepts) != 1 {
		t.Fatalf("got %d concepts, want 1", len(concepts))
	}
	item := concepts[0]
	if item.Concept != "chmod" || item.Attempts != 2 || item.Stage != 0 ||
		item.CurrentMisses != 1 || item.LastMisses != 0 || item.Status != progress.StatusReinforce {
		t.Fatalf("unexpected concept progress: %+v", item)
	}
	if item.LastReviewed.IsZero() || !item.NextReview.Equal(item.LastReviewed) {
		t.Fatalf("last review %v, review eligibility %v", item.LastReviewed, item.NextReview)
	}
	blocks, err := store.ReviewBlocks(context.Background(), "chmod")
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 || !blocks[0].Passed || blocks[0].Misses != 0 ||
		blocks[1].Passed || blocks[1].Misses != 1 {
		t.Fatalf("review blocks = %+v, want clean block followed by one open miss", blocks)
	}
}

func TestProgressSurvivesRestartAndDatabaseIsLocal(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	if err := initProgressStore(context.Background()); err != nil {
		t.Fatal(err)
	}
	record(t, "grep", false)
	record(t, "grep", true)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	if err := initProgressStore(context.Background()); err != nil {
		t.Fatal(err)
	}
	concepts, err := store.Concepts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(concepts) != 1 || concepts[0].Concept != "grep" ||
		concepts[0].Status != progress.StatusReview || concepts[0].Stage != 1 ||
		concepts[0].Attempts != 2 || concepts[0].CurrentMisses != 0 {
		t.Fatalf("after reopening database: %+v", concepts)
	}
	dbPath := filepath.Join(stateHome, "linux-mentor", "progress.db")
	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("database was not created at %s: %v", dbPath, err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("database permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestLearningStatusReflectsDueConceptsAndReviewEligibility(t *testing.T) {
	newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	_, err := store.RecordEvaluation(ctx, "chmod", true, "", "", "", "", now.AddDate(0, 0, -15))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.RecordEvaluation(ctx, "grep", false, "", "", "", "", now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.RecordEvaluation(ctx, "ls -la", false, "", "", "", "", now)
	if err != nil {
		t.Fatal(err)
	}

	_, out, err := getLearningStatus(ctx, nil, learningStatusInput{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out.DueForReview, []string{"chmod (stage 1)", "grep (stage 0)", "ls -la (stage 0)"}) {
		t.Errorf("eligible = %v", out.DueForReview)
	}
	if !reflect.DeepEqual(out.AreasToWorkOn, []string{"grep", "ls -la"}) {
		t.Errorf("areas = %v", out.AreasToWorkOn)
	}

	_, one, err := getLearningStatus(ctx, nil, learningStatusInput{Concept: " CHMOD "})
	if err != nil {
		t.Fatal(err)
	}
	if !one.ConceptFound || !one.OkForReview || one.Status != progress.StatusLearning {
		t.Errorf("single concept lookup: %+v", one)
	}
	_, missing, err := getLearningStatus(ctx, nil, learningStatusInput{Concept: "awk"})
	if err != nil {
		t.Fatal(err)
	}
	if missing.ConceptFound || missing.OkForReview {
		t.Errorf("unknown concept lookup: %+v", missing)
	}
}

func TestUserPreferencesCanBeReadAndSaved(t *testing.T) {
	newTestStore(t)
	ctx := context.Background()

	_, defaults, err := getUserPreferences(ctx, nil, statusInput{})
	if err != nil {
		t.Fatal(err)
	}
	if defaults.InteractionStyle != "socratic" || defaults.Tone != "encouraging" {
		t.Fatalf("unexpected defaults: %+v", defaults)
	}

	_, saved, err := saveUserPreferences(ctx, nil, saveUserPreferencesInput{
		InteractionStyle: "direct when requested",
		Tone:             "calm",
		ResponseLength:   "brief",
		AdditionalNotes:  "Prefer plain language.",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, loaded, err := getUserPreferences(ctx, nil, statusInput{})
	if err != nil {
		t.Fatal(err)
	}
	if saved.InteractionStyle != loaded.InteractionStyle ||
		saved.Tone != loaded.Tone ||
		saved.ResponseLength != loaded.ResponseLength ||
		saved.AdditionalNotes != loaded.AdditionalNotes {
		t.Fatalf("saved = %+v, loaded = %+v", saved, loaded)
	}
}

func TestSearchDocsUsesOnlyTheDetectedDistroSection(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(root, "docs.db")
	ubuntuRoot := filepath.Join(root, "ubuntu")
	fedoraRoot := filepath.Join(root, "fedora")
	writeReadme(t, ubuntuRoot, "Ubuntu documents package installation with apt.")
	writeReadme(t, fedoraRoot, "Fedora documents package installation with dnf.")

	ubuntu, err := docs.Open(ctx, dbPath, docs.Release{ID: "ubuntu"}, []string{ubuntuRoot})
	if err != nil {
		t.Fatal(err)
	}
	defer ubuntu.Close()
	fedora, err := docs.Open(ctx, dbPath, docs.Release{ID: "fedora"}, []string{fedoraRoot})
	if err != nil {
		t.Fatal(err)
	}
	defer fedora.Close()

	previous := docsStore
	docsStore = ubuntu
	t.Cleanup(func() { docsStore = previous })
	_, out, err := searchDocs(ctx, nil, searchDocsInput{Query: "package installation apt"})
	if err != nil {
		t.Fatal(err)
	}
	if out.DistroID != "ubuntu" || len(out.Results) != 1 ||
		!strings.Contains(out.Results[0].Snippet, "Ubuntu") {
		t.Fatalf("Ubuntu search output = %+v", out)
	}

	empty, err := docs.Open(ctx, dbPath, docs.Release{ID: "unknown-distro"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer empty.Close()
	docsStore = empty
	_, out, err = searchDocs(ctx, nil, searchDocsInput{Query: "package installation"})
	if err != nil {
		t.Fatal(err)
	}
	if out.DistroID != "unknown-distro" || len(out.Results) != 0 ||
		!strings.Contains(out.Message, "No local documentation") {
		t.Fatalf("empty distro section unexpectedly used another section: %+v", out)
	}
}

func writeReadme(t *testing.T, root, content string) {
	t.Helper()
	path := filepath.Join(root, "example", "README.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
