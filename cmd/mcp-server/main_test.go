package main

import (
	"context"
	"testing"
	"time"
)

var testNow = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

func grade(item *ReviewItem, results ...bool) {
	for _, passed := range results {
		applyEvaluation(item, passed, testNow)
	}
}

func daysUntilNext(item *ReviewItem) int {
	return int(item.NextReview.Sub(testNow).Hours() / 24)
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

type step struct {
	stage  int
	days   int
	status string
}

func runSteps(t *testing.T, answers []bool, steps []step) {
	t.Helper()
	item := &ReviewItem{Concept: "chmod"}
	for i, want := range steps {
		grade(item, answers...)
		if item.Stage != want.stage || item.Status != want.status || daysUntilNext(item) != want.days {
			t.Fatalf("round %d: stage %d, status %q, %d days; want stage %d, status %q, %d days",
				i+1, item.Stage, item.Status, daysUntilNext(item), want.stage, want.status, want.days)
		}
	}
}

func TestCorrectImmediatelyClimbsToMastery(t *testing.T) {
	runSteps(t, []bool{true}, []step{
		{1, 14, StatusLearning},
		{2, 28, StatusLearning},
		{3, 56, StatusMastered},
		{3, 56, StatusMastered}, // more correct answers maintain mastery
	})
}

func TestWrongOnceThenCorrect(t *testing.T) {
	runSteps(t, []bool{false, true}, []step{
		{1, 2, StatusReview},
		{2, 4, StatusReview},
		{3, 8, StatusReview},
		{3, 8, StatusReview}, // progression stops at three stages
	})
}

func TestWrongTwiceOrMoreThenCorrect(t *testing.T) {
	runSteps(t, []bool{false, false, true}, []step{
		{1, 1, StatusReinforce},
		{2, 2, StatusReinforce},
		{3, 4, StatusReinforce},
	})
	runSteps(t, []bool{false, false, false, true}, []step{
		{1, 1, StatusReinforce}, // three wrong answers still count as "twice or more"
	})
}

func TestChangingCategoryResetsTheStage(t *testing.T) {
	item := &ReviewItem{Concept: "chmod"}
	grade(item, true, true) // clean, stage 2
	grade(item, false, true) // wrong once: new category, back to stage 1
	if item.Stage != 1 || item.Status != StatusReview || daysUntilNext(item) != 2 {
		t.Fatalf("got stage %d, status %q, %d days", item.Stage, item.Status, daysUntilNext(item))
	}
	grade(item, true) // clean again: another change, stage 1 with the clean interval
	if item.Stage != 1 || item.Status != StatusLearning || daysUntilNext(item) != 14 {
		t.Fatalf("got stage %d, status %q, %d days", item.Stage, item.Status, daysUntilNext(item))
	}
}

func TestAMissBreaksTheMasteryStreak(t *testing.T) {
	item := &ReviewItem{Concept: "grep"}
	grade(item, true, true)  // two first-try successes
	grade(item, false, true) // a miss resets the streak
	grade(item, true, true)  // two clean answers are not enough yet
	if item.Status == StatusMastered {
		t.Fatal("mastered after only two clean answers following a miss")
	}
	grade(item, true) // the third consecutive first-try success
	if item.Status != StatusMastered {
		t.Fatalf("status = %q, want mastered after three clean answers in a row", item.Status)
	}
}

func TestUnresolvedMissNeedsReinforcementAndIsEligibleNow(t *testing.T) {
	item := &ReviewItem{Concept: "ls -la"}
	grade(item, false)
	if item.Status != StatusReinforce || item.CurrentMisses != 1 {
		t.Fatalf("status %q, misses %d", item.Status, item.CurrentMisses)
	}
	if !shouldReview(item, testNow) {
		t.Fatal("an unresolved concept should be eligible immediately")
	}
}

func TestMissCounterResetsAfterACorrectAnswer(t *testing.T) {
	item := &ReviewItem{Concept: "chmod"}
	grade(item, false, false, true)
	if item.CurrentMisses != 0 || item.Attempts != 3 {
		t.Fatalf("misses %d, attempts %d, want 0 and 3", item.CurrentMisses, item.Attempts)
	}
}

// --- the tools, using a temporary progress file ---

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
		t.Fatalf("recordEvaluation(%q, %v): success=%v err=%v", concept, passed, out.Success, err)
	}
}

func TestRecordEvaluationNormalizesConceptNames(t *testing.T) {
	newTestStore(t)
	record(t, "  CHMOD ", true)
	record(t, "chmod", true)

	if len(store.Items) != 1 {
		t.Fatalf("got %d concepts, want 1", len(store.Items))
	}
	if item := store.Items["chmod"]; item == nil || item.Attempts != 2 || item.Stage != 2 {
		t.Fatalf("got %+v, want chmod with 2 attempts at stage 2", item)
	}
}

func TestProgressSurvivesARestart(t *testing.T) {
	newTestStore(t)
	record(t, "grep", false)
	record(t, "grep", true)

	initProgressStore() // reload from the file the server just wrote

	item := store.Items["grep"]
	if item == nil || item.Status != StatusReview || item.Stage != 1 {
		t.Fatalf("after reload: %+v", item)
	}
}

func TestLearningStatusListsAndEligibility(t *testing.T) {
	newTestStore(t)
	record(t, "chmod", true)
	record(t, "chmod", true)
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
	if !sameStrings(out.Mastered, []string{"chmod"}) || !sameStrings(out.AreasToWorkOn, []string{"ls -la"}) {
		t.Errorf("mastered = %v, areas = %v", out.Mastered, out.AreasToWorkOn)
	}
	if len(out.DueForReview) != 0 {
		t.Errorf("eligible = %v, want none yet", out.DueForReview)
	}

	// move grep's eligibility date into the past
	store.mu.Lock()
	store.Items["grep"].NextReview = time.Now().Add(-time.Hour)
	store.mu.Unlock()

	_, out, _ = getLearningStatus(ctx, nil, learningStatusInput{})
	if !sameStrings(out.DueForReview, []string{"grep (stage 1)"}) {
		t.Errorf("eligible = %v, want [grep (stage 1)]", out.DueForReview)
	}

	_, one, _ := getLearningStatus(ctx, nil, learningStatusInput{Concept: " GREP "})
	if !one.ConceptFound || !one.OkForReview || one.Status != StatusReview {
		t.Errorf("single concept lookup: %+v", one)
	}
	_, missing, _ := getLearningStatus(ctx, nil, learningStatusInput{Concept: "awk"})
	if missing.ConceptFound || missing.OkForReview {
		t.Errorf("unknown concept lookup: %+v", missing)
	}
}