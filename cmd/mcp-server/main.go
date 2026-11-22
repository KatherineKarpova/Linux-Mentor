// this file defines tools the mcp server can call to retrieve documentation and track a user's learning progress 

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// input and output types for the server connection check
type statusInput struct{}	
type statusOutput struct {
	Message string `json:"message"`
}

// return a message confirming that the mcp server is connected
func getStatus(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input statusInput,
) (*mcp.CallToolResult, statusOutput, error) {
	return nil, statusOutput{
		Message: "Linux-Mentor MCP server connected",
	}, nil
}

// stores learning progress and review scheduling for one linux concept
type ReviewItem struct {
	Concept				string		`json:"concept"`
	Stage				int       	`json:"stage"`			// tracks the interval out of 3 for the status category
	LastMisses       	int       	`json:"last_misses"` 	// wrong answers before the last correct answer, capped at 2 (0 = clean)
	LastReviewed     	time.Time 	`json:"last_reviewed"`
	NextReview       	time.Time 	`json:"next_review"`	// the concept is eligible to come up again after this time
	CommandAccuracy  	string   	`json:"command_accuracy,omitempty"`
	ExplanationDepth 	string    	`json:"explanation_depth,omitempty"`
	Notes            	string    	`json:"notes,omitempty"`
	Status           	string    	`json:"status"`   
	Attempts         	int       	`json:"attempts"` // total graded answers for this concept
	CurrentMisses       int       	`json:"current_misses"`   // wrong answers since the last correct answer
}

// learning statuses based on mistake before a correct answer
const (
	StatusMastered  = "mastered"  // correct with 0 wrong answers first
	StatusLearning  = "learning"  // first-try successes so far, fewer than three in a row
	StatusReview    = "review"    // 1 wrong answer, then correct
	StatusReinforce = "reinforce" // 2 or more wrong answers, then correct
)

const maxStage = 3

// intervalDays[bucket][stage-1] is how many days to wait before the concept is eligible again.
// bucket 0 = correct with no wrong answers first, 1 = one wrong answer first, 2 = two or more.
// within a bucket, each stage doubles the one before it.
var intervalDays = [3][maxStage]int{
	{14, 28, 56},
	{2, 4, 8},
	{1, 2, 4},
}

// caps wrong answers at 2
func missBucket(misses int) int {
	if misses > 2 {
		return 2
	}
	return misses
}

// picks the status the learner sees
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

// calculate the next review date based on the assigned stage
func nextReviewFor(bucket, stage int, now time.Time) time.Time {
	return now.AddDate(0, 0, intervalDays[bucket][stage-1])
}

// check if a concept is eligble for another review
func shouldReview(item *ReviewItem, now time.Time) bool {
	if item == nil {
		return false
	}
	return !now.Before(item.NextReview)
}

func normalizeConcept(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "general-linux"
	}
	return name
}

func (s *ProgressStore) getOrCreateItem(concept string) *ReviewItem {
	concept = strings.ToLower(strings.TrimSpace(concept))
	item, exists := store.Items[concept]
	if !exists {
		item = &ReviewItem{Concept: concept}
		store.Items[concept] = item
	}
	return item 
}

// applyEvaluation updates one concept after the learner answers a question.
func applyEvaluation(item *ReviewItem, passed bool, now time.Time) {

	item.Attempts++
	item.LastReviewed = now

	if !passed {
		// keep counting wrong answers until the learner gets it right.
		// the stage and last result stay as they were until then.
		item.CurrentMisses++
		item.Stage = 0
		item.Status = StatusReinforce // unresolved, needs work until answered correctly
		item.NextReview = now
		return
	}

	bucket := missBucket(item.CurrentMisses)
	item.CurrentMisses = 0

	if item.Stage >= 1 && bucket == item.LastMisses {
		// the same kind of result again: move up a stage, up to the limit
		if item.Stage < maxStage {
			item.Stage++
		}
	} else {
		// a different kind of result (or the first one): restart at stage 1
		item.Stage = 1
	}
	item.LastMisses = bucket
	item.Status = statusFor(bucket, item.Stage)
	item.NextReview = nextReviewFor(bucket, item.Stage, now)
}

type ProgressStore struct {
	mu       sync.RWMutex
	filePath string
	Items    map[string]*ReviewItem `json:"items"`
}

var store *ProgressStore

func initProgressStore() {
	stateDir := os.Getenv("XDG_STATE_HOME")
	if stateDir == "" {
		home, _ := os.UserHomeDir()
		stateDir = filepath.Join(home, ".local", "state")
	}
	mentorDir := filepath.Join(stateDir, "linux-mentor")
	_ = os.MkdirAll(mentorDir, 0755)

	store = &ProgressStore{
		filePath: filepath.Join(mentorDir, "progress.json"),
		Items:    make(map[string]*ReviewItem),
	}

	data, err := os.ReadFile(store.filePath)
	if err == nil {
		var raw map[string]*ReviewItem
		if err := json.Unmarshal(data, &raw); err == nil && raw != nil {
			for key, item := range raw {
				if item == nil { // a JSON null would otherwise crash below
					delete(raw, key)
					continue
				}
				store.Items = raw
			}
	} else if os.IsNotExist(err) {
		_ = store.save()
	}
}}

func (s *ProgressStore) save() error {
	data, err := json.MarshalIndent(s.Items, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.filePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.filePath)
}

type searchDocsInput struct {
	Distro string `json:"distro" jsonschema:"Target Linux distribution: ubuntu or fedora"`
	Query  string `json:"query" jsonschema:"Command or concept keyword to search in offline docs"`
}

type docResult struct {
	Title    string `json:"title"`
	Distro   string `json:"distro"`
	Category string `json:"category"`
	Snippet  string `json:"snippet"`
}

type searchDocsOutput struct {
	Results []docResult `json:"results"`
	Message string      `json:"message"`
}

func searchDocs(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input searchDocsInput,
) (*mcp.CallToolResult, searchDocsOutput, error) {
	distro := strings.ToLower(strings.TrimSpace(input.Distro))
	query := strings.ToLower(strings.TrimSpace(input.Query))

	if distro == "" {
		distro = "ubuntu"
	}

	builtInDocs := []docResult{
		{
			Title:    "apt",
			Distro:   "ubuntu",
			Category: "package-management",
			Snippet:  "apt is the high-level package management tool for Ubuntu. 'apt update' refreshes package lists; 'apt install <pkg>' installs packages.",
		},
		{
			Title:    "dnf",
			Distro:   "fedora",
			Category: "package-management",
			Snippet:  "dnf is the next-generation package manager for Fedora. 'dnf check-update' checks updates; 'dnf install <pkg>' installs packages.",
		},
		{
			Title:    "systemctl",
			Distro:   "ubuntu",
			Category: "service-management",
			Snippet:  "systemctl inspects and controls systemd services. Use 'systemctl status <unit>' to verify service state.",
		},
		{
			Title:    "systemctl",
			Distro:   "fedora",
			Category: "service-management",
			Snippet:  "systemctl controls systemd services on Fedora. Combined with 'journalctl -u <unit>' for troubleshooting unit failures.",
		},
		{
			Title:    "journalctl",
			Distro:   "fedora",
			Category: "troubleshooting",
			Snippet:  "journalctl queries the systemd journal. Use 'journalctl -xe' for catalog errors or 'journalctl -f' to follow logs.",
		},
		{
			Title:    "journalctl",
			Distro:   "ubuntu",
			Category: "troubleshooting",
			Snippet:  "journalctl queries the systemd journal on Ubuntu. Use 'journalctl -u <service>' to inspect service-specific log output.",
		},
	}

	var matched []docResult
	for _, doc := range builtInDocs {
		if (doc.Distro == distro || distro == "all") &&
			(strings.Contains(strings.ToLower(doc.Title), query) || strings.Contains(strings.ToLower(doc.Snippet), query)) {
			matched = append(matched, doc)
		}
	}

	msg := fmt.Sprintf("Found %d documentation entries for '%s' on %s", len(matched), query, distro)
	return nil, searchDocsOutput{
		Results: matched,
		Message: msg,
	}, nil
}

type learningStatusInput struct {
	Concept string `json:"concept,omitempty" jsonschema:"Optional specific concept to inspect"`
}

type learningStatusOutput struct {
	DueForReview   	[]string 	`json:"due_for_review"`	// eligible if the topic comes up naturally
	Mastered       	[]string 	`json:"mastered"`
	AreasToWorkOn  	[]string 	`json:"areas_to_work_on"`
	TotalPracticed 	int      	`json:"total_practiced"`
	Message        	string   	`json:"message"`
	ConceptFound	bool 		`json:"concept_found"`
	OkForReview		bool 		`json:"ok_for_review"`
	Status         	string   	`json:"status,omitempty"`
	Stage          	int      	`json:"stage,omitempty"`
	NextReview     	string   	`json:"next_review,omitempty"`
}

// 
func getLearningStatus(ctx context.Context,
	req *mcp.CallToolRequest,
	input learningStatusInput,
) (*mcp.CallToolResult, learningStatusOutput, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()

	now := time.Now()
	// empty slices, not nil, so the tool's JSON output has arrays and never null
	out := learningStatusOutput{
		DueForReview:  []string{},
		Mastered:      []string{},
		AreasToWorkOn: []string{},
	}

	// one concept: tell the mentor whether it is eligible to come up
	if concept := strings.ToLower(strings.TrimSpace(input.Concept)); concept != "" {
		item, exists := store.Items[concept]
		if !exists {
			out.Message = fmt.Sprintf("'%s' has not been practiced yet", concept)
			return nil, out, nil
		}
		out.ConceptFound = true
		out.OkForReview = shouldReview(item, now)
		out.Status = item.Status
		out.Stage = item.Stage
		out.NextReview = item.NextReview.Format("2006-01-02")
		out.Message = fmt.Sprintf("'%s' is %s (stage %d), eligible after %s",
			concept, item.Status, item.Stage, out.NextReview)
		return nil, out, nil
	}

	// overall summary
	for concept, item := range store.Items {
		if item.Status == StatusMastered {
			out.Mastered = append(out.Mastered, concept)
		}
		if item.Status == StatusReinforce {
			out.AreasToWorkOn = append(out.AreasToWorkOn, concept)
		}
		if shouldReview(item, now) {
			out.DueForReview = append(out.DueForReview, fmt.Sprintf("%s (stage %d)", concept, item.Stage))
		}
	}
	// sort concepts alhabetically for consistency
	sort.Strings(out.DueForReview)
	sort.Strings(out.Mastered)
	sort.Strings(out.AreasToWorkOn)

	out.TotalPracticed = len(store.Items)
	out.Message = fmt.Sprintf("Tracking %d concepts (%d mastered, %d eligible if they come up naturally)",
		len(store.Items), len(out.Mastered), len(out.DueForReview))
	return nil, out, nil
}

type recordEvaluationInput struct {
	Concept          string `json:"concept" jsonschema:"The Linux topic or command practiced"`
	Passed           bool   `json:"passed" jsonschema:"True if learner demonstrated command accuracy and explanation depth"`
	CommandAccuracy  string `json:"command_accuracy" jsonschema:"Evaluation notes on command correctness"`
	ExplanationDepth string `json:"explanation_depth" jsonschema:"Evaluation notes on conceptual explanation depth"`
	Notes            string `json:"notes,omitempty" jsonschema:"Additional mentor notes or observations"`
}

type recordEvaluationOutput struct {
	Success    bool   `json:"success"`
	Concept    string `json:"concept"`
	NewStage   int    `json:"new_stage"`
	NextReview string `json:"next_review"`
	Message    string `json:"message"`
}

func recordEvaluation(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input recordEvaluationInput,
) (*mcp.CallToolResult, recordEvaluationOutput, error) {
	store.mu.Lock()
	defer store.mu.Unlock()

	
	concept := normalizeConcept(input.Concept)
	item := store.getOrCreateItem(concept)

	now := time.Now()
	item.CommandAccuracy = input.CommandAccuracy
	item.ExplanationDepth = input.ExplanationDepth
	item.Notes = input.Notes
	applyEvaluation(item, input.Passed, now)

	_ = store.save()

	if err := store.save(); err != nil {
		return nil, recordEvaluationOutput{}, err
	}

	msg := fmt.Sprintf("Recorded evaluation for '%s'. Stage is now %d (next review: %s)",
		concept, item.Stage, item.NextReview.Format("2006-01-02"))

	return nil, recordEvaluationOutput{
		Success:    true,
		Concept:    concept,
		NewStage:   item.Stage,
		NextReview: item.NextReview.Format(time.RFC3339),
		Message:    msg,
	}, nil
}

func main() {
	initProgressStore()

	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "linux-mentor",
			Version: "0.2.0",
		},
		nil,
	)

	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "get_status",
			Description: "Check Linux-Mentor server status",
		},
		getStatus,
	)

	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "search_docs",
			Description: "Search offline documentation across Ubuntu and Fedora distributions",
		},
		searchDocs,
	)

	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "get_learning_status",
			Description: "Inspect user learning progress, mastered concepts, and spaced review queue",
		},
		getLearningStatus,
	)

	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "record_evaluation",
			Description: "Record command accuracy and explanation depth milestones in spaced repetition",
		},
		recordEvaluation,
	)

	if err := server.Run(
		context.Background(),
		&mcp.StdioTransport{},
	); err != nil {
		log.Fatal(err)
	}
}
