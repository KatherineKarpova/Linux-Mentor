package main

import (
	"context"
	"testing"
	"time"
)

var testNow = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

// gradeSequence applies a series of graded answers to a brand-new concept.
func gradeSequence(concept string, results ...bool) *ReviewItem {
	item := &ReviewItem{Concept: concept}
	for _, passed := range results {
		applyEvaluation(item, passed, testNow)
	}
	return item
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- the three rules --------------------------------------------------------

func TestApplyEvaluationRules(t *testing.T) {
	tests := []struct {
		name       string
		results    []bool
		wantStatus string
		wantStage  int
		wantNext   time.Time
	}{
		{"correct right away", []bool{true}, StatusMastered, 3, testNow.AddDate(0, 3, 0)},
		{"wrong once, then correct", []bool{false, true}, StatusReview, 1, testNow.AddDate(0, 0, 7)},
		{"wrong twice, then correct", []bool{false, false, true}, StatusReinforce, 0, testNow.AddDate(0, 0, 1)},
		{"wrong three times, then correct", []bool{false, false, false, true}, StatusReinforce, 0, testNow.AddDate(0, 0, 1)},
		{"wrong once, still unresolved", []bool{false}, StatusReinforce, 0, testNow},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			item := gradeSequence("chmod", tc.results...)
			if item.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", item.Status, tc.wantStatus)
			}
			if item.Stage != tc.wantStage {
				t.Errorf("stage = %d, want %d", item.Stage, tc.wantStage)
			}
			if !item.NextReview.Equal(tc.wantNext) {
				t.Errorf("next review = %v, want %v", item.NextReview, tc.wantNext)
			}
		})
	}
}

// --- counters ---------------------------------------------------------------

func TestMissesResetAfterACorrectAnswer(t *testing.T) {
	item := gradeSequence("chmod", false, false, true)
	if item.Misses != 0 {
		t.Errorf("misses = %d, want 0 after a correct answer", item.Misses)
	}
	if item.Attempts != 3 {
		t.Errorf("attempts = %d, want 3", item.Attempts)
	}
}

// --- what happens on later reviews ------------------------------------------

func TestCleanLaterReviewsClimbTheStages(t *testing.T) {
	item := &ReviewItem{Concept: "grep", Stage: 1, Attempts: 1, Status: StatusReview}

	applyEvaluation(item, true, testNow)
	if item.Stage != 2 || item.Status != StatusReview || !item.NextReview.Equal(testNow.AddDate(0, 2, 0)) {
		t.Fatalf("after first clean review: stage %d, status %q, next %v", item.Stage, item.Status, item.NextReview)
	}

	applyEvaluation(item, true, testNow)
	if item.Stage != 3 || item.Status != StatusMastered || !item.NextReview.Equal(testNow.AddDate(0, 3, 0)) {
		t.Fatalf("after second clean review: stage %d, status %q, next %v", item.Stage, item.Status, item.NextReview)
	}
}

func TestReinforcedConceptRecoversAfterACleanReview(t *testing.T) {
	// wrong twice then correct lands in Areas to Work On; a later clean answer moves it to the queue
	item := gradeSequence("ls -la", false, false, true, true)
	if item.Status != StatusReview || item.Stage != 1 {
		t.Fatalf("status %q stage %d, want review at stage 1", item.Status, item.Stage)
	}
}

func TestMasteredConceptThatIsMissedLaterNeedsReinforcement(t *testing.T) {
	item := gradeSequence("chmod", true)
	applyEvaluation(item, false, testNow)
	if item.Status != StatusReinforce || item.Stage != 0 || item.Misses != 1 || item.Attempts != 2 {
		t.Fatalf("got status %q stage %d misses %d attempts %d", item.Status, item.Stage, item.Misses, item.Attempts)
	}
}

func TestStatusFromStage(t *testing.T) {
	tests := map[int]string{-1: StatusReinforce, 0: StatusReinforce, 1: StatusReview, 2: StatusReview, 3: StatusMastered, 4: StatusMastered}
	for stage, want := range tests {
		if got := statusFromStage(stage); got != want {
			t.Errorf("statusFromStage(%d) = %q, want %q", stage, got, want)
		}
	}
}

// --- the tools, using a temporary progress file -----------------------------

func newTestStore(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	initProgressStore()
}

func record(t *testing.T, concept string, passed bool) {
	t.Helper()
	_, out, err := recordEvaluation(context.Background(), nil, recordEvaluationInput{
		Concept: concept,
		Passed:  passed,
	})
	if err != nil || !out.Success {
		t.Fatalf("recordEvaluation(%q, %v) failed: success=%v err=%v", concept, passed, out.Success, err)
	}
}

func TestRecordEvaluationNormalizesConceptNames(t *testing.T) {
	newTestStore(t)
	record(t, "  CHMOD ", true)
	record(t, "chmod", true)

	if len(store.Items) != 1 {
		t.Fatalf("got %d concepts, want 1 (names should be trimmed and lower-cased)", len(store.Items))
	}
	item := store.Items["chmod"]
	if item == nil || item.Attempts != 2 {
		t.Fatalf("expected one concept named chmod with 2 attempts, got %+v", item)
	}
}

func TestProgressSurvivesARestart(t *testing.T) {
	newTestStore(t)
	record(t, "grep", false)
	record(t, "grep", true)

	initProgressStore() // reload from the file the server just wrote

	item := store.Items["grep"]
	if item == nil {
		t.Fatal("grep was not saved to disk")
	}
	if item.Status != StatusReview || item.Stage != 1 || item.Attempts != 2 {
		t.Fatalf("after reload: status %q stage %d attempts %d", item.Status, item.Stage, item.Attempts)
	}
}

func TestLearningStatusListsAreDisjoint(t *testing.T) {
	newTestStore(t)
	record(t, "chmod", true)
	record(t, "grep", false)
	record(t, "grep", true)
	record(t, "ls -la", false)
	record(t, "ls -la", false)
	record(t, "ls -la", true)

	ctx := context.Background()
	_, out, err := getLearningStatus(ctx, nil, learningStatusInput{})
	if err != nil {
		t.Fatal(err)
	}
	if !sameStrings(out.Mastered, []string{"chmod"}) {
		t.Errorf("mastered = %v, want [chmod]", out.Mastered)
	}
	if !sameStrings(out.AreasToWorkOn, []string{"ls -la"}) {
		t.Errorf("areas = %v, want [ls -la]", out.AreasToWorkOn)
	}
	if len(out.DueForReview) != 0 {
		t.Errorf("due = %v, want none yet (grep is scheduled a week out)", out.DueForReview)
	}
	if out.TotalPracticed != 3 {
		t.Errorf("total practiced = %d, want 3", out.TotalPracticed)
	}

	// move grep's review date into the past
	store.mu.Lock()
	store.Items["grep"].NextReview = time.Now().Add(-time.Hour)
	store.mu.Unlock()

	_, out, err = getLearningStatus(ctx, nil, learningStatusInput{})
	if err != nil {
		t.Fatal(err)
	}
	if !sameStrings(out.DueForReview, []string{"grep (stage 1)"}) {
		t.Errorf("due = %v, want [grep (stage 1)]", out.DueForReview)
	}
}
