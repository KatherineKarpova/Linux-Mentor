package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"sort"
)

type statusInput struct{}

type statusOutput struct {
	Message string `json:"message"`
}

func getStatus(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input statusInput,
) (*mcp.CallToolResult, statusOutput, error) {
	return nil, statusOutput{
		Message: "Linux-Mentor MCP server connected",
	}, nil
}

// ReviewItem models spaced repetition progress for a single concept or command.
// Intervals:
// Stage 1: 7 days
// Stage 2: 2 months (approx 60 days)
// Stage 3: 3 months (approx 90 days)
// On failure: fall back to previous stage.
type ReviewItem struct {
	Concept          string    `json:"concept"`
	Stage            int       `json:"stage"`
	LastReviewed     time.Time `json:"last_reviewed"`
	NextReview       time.Time `json:"next_review"`
	CommandAccuracy  string    `json:"command_accuracy,omitempty"`
	ExplanationDepth string    `json:"explanation_depth,omitempty"`
	Notes            string    `json:"notes,omitempty"`
	Status       	 string    `json:"status"`   // exactly one of the Status constants below
	Attempts         int       `json:"attempts"` // total graded answers for this concept
	Misses           int       `json:"misses"`   // wrong answers since the last correct one
}

// Every concept is in exactly one of these at a time.
const (
	StatusMastered  = "mastered"  // correct on the first try
	StatusReview    = "review"    // wrong once, then correct: spaced review queue
	StatusReinforce = "reinforce" // wrong twice or more, or still unresolved
)

func statusFromStage(stage int) string {
	switch {
	case stage >= 3:
		return StatusMastered
	case stage <= 0:
		return StatusReinforce
	default:
		return StatusReview
	}
}

func nextReviewFor(stage int, now time.Time) time.Time {
	switch stage {
	case 0:
		return now.AddDate(0, 0, 1) // reinforce: try again tomorrow
	case 1:
		return now.AddDate(0, 0, 7)
	case 2:
		return now.AddDate(0, 2, 0)
	default:
		return now.AddDate(0, 3, 0)
	}
}

// applyEvaluation updates one concept after the learner answers a question.
func applyEvaluation(item *ReviewItem, passed bool, now time.Time) {
	firstAttempt := item.Attempts == 0
	item.Attempts++
	item.LastReviewed = now

	if !passed {
		item.Misses++
		item.Stage = 0
		item.Status = StatusReinforce // unresolved: needs work until answered correctly
		item.NextReview = now
		return
	}

	misses := item.Misses
	item.Misses = 0

	switch {
	case firstAttempt:
		item.Stage = 3
		item.Status = StatusMastered
	case misses == 0:
		// a clean later review moves the concept up one stage
		if item.Stage < 3 {
			item.Stage++
		}
		item.Status = statusFromStage(item.Stage)
	case misses == 1:
		item.Stage = 1
		item.Status = StatusReview
	default:
		item.Stage = 0
		item.Status = StatusReinforce
	}
	item.NextReview = nextReviewFor(item.Stage, now)
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
			store.Items = raw
			for key,item := range store.Items {
				if item == nil { // a JSON null would otherwise crash below
					delete(store.Items, key)
					continue
				}
				if item.Status == "" {
					item.Status = statusFromStage(item.Stage)
				}
				if item.Attempts == 0 && !item.LastReviewed.IsZero() {
					item.Attempts = 1
				}
			}
		}   
		} else if os.IsNotExist(err) {
			_ = store.save()
		}
	}

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
	DueForReview   []string `json:"due_for_review"`
	Mastered       []string `json:"mastered"`
	AreasToWorkOn  []string `json:"areas_to_work_on"`
	TotalPracticed int      `json:"total_practiced"`
	Message        string   `json:"message"`
}

func getLearningStatus(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input learningStatusInput,
) (*mcp.CallToolResult, learningStatusOutput, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()

	now := time.Now()
	var due, mastered, areas []string

	for concept, item := range store.Items {
		switch item.Status {
		case StatusMastered:
			mastered = append(mastered, concept)
		case StatusReview:
			if !now.Before(item.NextReview) {
				due = append(due, fmt.Sprintf("%s (stage %d)", concept, item.Stage))
			}
		case StatusReinforce:
			areas = append(areas, concept)
		}
	}
	sort.Strings(due)
	sort.Strings(mastered)
	sort.Strings(areas)

	msg := fmt.Sprintf("Tracking %d concepts (%d mastered, %d due for spaced review)", len(store.Items), len(mastered), len(due))
	return nil, learningStatusOutput{
		DueForReview:   due,
		Mastered:       mastered,
		AreasToWorkOn:  areas,
		TotalPracticed: len(store.Items),
		Message:        msg,
	}, nil
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

	concept := strings.TrimSpace(input.Concept)
	if concept == "" {
		concept = "general-linux"
	}

	item, exists := store.Items[concept]
	if !exists {
		item = &ReviewItem{Concept: concept}
		store.Items[concept] = item
	}

	now := time.Now()
	item.CommandAccuracy = input.CommandAccuracy
	item.ExplanationDepth = input.ExplanationDepth
	item.Notes = input.Notes
	applyEvaluation(item, input.Passed, now)

	_ = store.save()

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
