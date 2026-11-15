package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type App struct {
	mu             sync.RWMutex
	conversationID string
	transcriptPath string
	offset         int64
	pending        []byte
	fileInfo       os.FileInfo
	messages       []MentorMessage
	followNewest   bool
}

type SessionInfo struct {
	ConversationID string `json:"conversationId"`
}

type MentorMessage struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	CreatedAt string `json:"createdAt"`
}

type transcriptRecord struct {
	Type      string `json:"type"`
	Source    string `json:"source"`
	Status    string `json:"status"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

func NewApp() *App {
	return &App{}
}

// newestTranscript returns the most recently modified Antigravity transcript, or "".
func newestTranscript() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	pattern := filepath.Join(home, ".gemini", "antigravity-cli", "brain", "*", ".system_generated", "logs", "transcript.jsonl")
	matches, _ := filepath.Glob(pattern)
	var newest string
	var newestTime time.Time
	for _, m := range matches {
		if info, err := os.Stat(m); err == nil && info.ModTime().After(newestTime) {
			newest, newestTime = m, info.ModTime()
		}
	}
	return newest
}

func (a *App) startup(ctx context.Context) {
	a.conversationID = os.Getenv("LINUX_MENTOR_CONVERSATION_ID")
	a.transcriptPath = os.Getenv("LINUX_MENTOR_TRANSCRIPT_PATH")

	a.followNewest = a.transcriptPath == ""

	go func() {
		ticker := time.NewTicker(400 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := a.readTranscript(); err != nil {
					log.Printf("linux-mentor: read transcript: %v", err)
				}
			}
		}
	}()
}

func (a *App) GetSessionInfo() SessionInfo {
	return SessionInfo{ConversationID: a.conversationID}
}

func (a *App) GetMessages() []MentorMessage {
	a.mu.RLock()
	defer a.mu.RUnlock()

	messages := make([]MentorMessage, len(a.messages))
	copy(messages, a.messages)
	return messages
}

type ReviewProgressItem struct {
	Concept          string `json:"concept"`
	Stage            int    `json:"stage"`
	LastReviewed     string `json:"lastReviewed"`
	NextReview       string `json:"nextReview"`
	CommandAccuracy  string `json:"commandAccuracy,omitempty"`
	ExplanationDepth string `json:"explanationDepth,omitempty"`
	Notes            string `json:"notes,omitempty"`
}

type LearningProgress struct {
	DueForReview  []ReviewProgressItem `json:"dueForReview"`
	Mastered      []ReviewProgressItem `json:"mastered"`
	AreasToWorkOn []ReviewProgressItem `json:"areasToWorkOn"`
	AllItems      []ReviewProgressItem `json:"allItems"`
}

func formatDay(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

func (a *App) GetLearningProgress() LearningProgress {
	result := LearningProgress{
		DueForReview:  []ReviewProgressItem{},
		Mastered:      []ReviewProgressItem{},
		AreasToWorkOn: []ReviewProgressItem{},
		AllItems:      []ReviewProgressItem{},
	}

	stateDir := os.Getenv("XDG_STATE_HOME")
	if stateDir == "" {
		home, _ := os.UserHomeDir()
		stateDir = filepath.Join(home, ".local", "state")
	}
	data, err := os.ReadFile(filepath.Join(stateDir, "linux-mentor", "progress.json"))
	if err != nil {
		return result
	}

	type rawItem struct {
		Concept          string    `json:"concept"`
		Stage            int       `json:"stage"`
		LastReviewed     time.Time `json:"last_reviewed"`
		NextReview       time.Time `json:"next_review"`
		CommandAccuracy  string    `json:"command_accuracy,omitempty"`
		ExplanationDepth string    `json:"explanation_depth,omitempty"`
		Notes            string    `json:"notes,omitempty"`
		Status           string    `json:"status"`
	}

	var raw map[string]*rawItem
	if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
		return result
	}

	// Go randomizes map order, so sort the keys to keep the lists stable.
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		item := raw[key]
		if item == nil {
			continue
		}
		status := item.Status
		if status == "" { // progress saved before statuses existed
			switch {
			case item.Stage >= 3:
				status = "mastered"
			case item.Stage <= 0:
				status = "reinforce"
			default:
				status = "review"
			}
		}
		concept := item.Concept
		if concept == "" {
			concept = key
		}
		entry := ReviewProgressItem{
			Concept:          concept,
			Stage:            item.Stage,
			LastReviewed:     formatDay(item.LastReviewed),
			NextReview:       formatDay(item.NextReview),
			CommandAccuracy:  item.CommandAccuracy,
			ExplanationDepth: item.ExplanationDepth,
			Notes:            item.Notes,
		}
		result.AllItems = append(result.AllItems, entry)

		// each concept goes in exactly one list
		switch status {
		case "mastered":
			result.Mastered = append(result.Mastered, entry)
		case "reinforce":
			result.AreasToWorkOn = append(result.AreasToWorkOn, entry)
		default:
			result.DueForReview = append(result.DueForReview, entry)
		}
	}
	return result
}

func (a *App) readTranscript() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.followNewest {
		if newest := newestTranscript(); newest != "" && newest != a.transcriptPath {
			a.transcriptPath = newest
			a.conversationID = filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(newest))))
			a.offset = 0
			a.pending = nil
			a.fileInfo = nil
			a.messages = nil
		}
	}
	info, err := os.Stat(a.transcriptPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	if a.fileInfo == nil || !os.SameFile(a.fileInfo, info) || info.Size() < a.offset {
		a.offset = 0
		a.pending = nil
		a.fileInfo = info
	}

	file, err := os.Open(a.transcriptPath)
	if err != nil {
		return err
	}
	defer file.Close()

	if _, err := file.Seek(a.offset, io.SeekStart); err != nil {
		return err
	}

	reader := bufio.NewReader(file)
	for {
		line, readErr := reader.ReadBytes('\n')
		a.offset += int64(len(line))
		a.pending = append(a.pending, line...)

		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return readErr
		}

		if errors.Is(readErr, io.EOF) {
			return nil
		}

		recordLine := strings.TrimSpace(string(a.pending))
		a.pending = a.pending[:0]
		if recordLine == "" {
			continue
		}
		if err := a.appendAssistantMessage([]byte(recordLine)); err != nil {
			return err
		}
	}
}

func (a *App) appendAssistantMessage(line []byte) error {
	var record transcriptRecord
	if err := json.Unmarshal(line, &record); err != nil {
		return err
	}

	if record.Type != "PLANNER_RESPONSE" || record.Source != "MODEL" || record.Status != "DONE" {
		return nil
	}

	content := strings.TrimSpace(record.Content)
	if content == "" {
		return nil
	}

	a.messages = append(a.messages, MentorMessage{
		ID:        record.CreatedAt + "-" + time.Now().Format(time.RFC3339Nano),
		Content:   content,
		CreatedAt: record.CreatedAt,
	})
	return nil
}
