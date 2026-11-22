// this file reads transcripts from the cli agent and send information to the Wails frontend


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

// stores the active agy session and transcript reader state
type App struct {
	mu             sync.RWMutex		// protects shared transcript data from concurrent access
	conversationID string			
	transcriptPath string			
	offset         int64			// number of transcript bytes already read
	pending        []byte			// incomplete transcript line
	fileInfo       os.FileInfo		// metadata for detecting file changes
	messages       []MentorMessage
	followNewest   bool				// follow the newest transcript if no path was supplied
}

// session info sent to the frontend
type SessionInfo struct {
	ConversationID string `json:"conversationId"`
}


// record format used when reading the agy transcript
type transcriptRecord struct {
	Type      string `json:"type"`
	Source    string `json:"source"`
	Status    string `json:"status"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

// message displayed in the desktop ui once record is prepared
type MentorMessage struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	CreatedAt string `json:"createdAt"`
}

// create a new app instance with default values being zero/empty
func NewApp() *App {
	return &App{}
}

// find the most recently modified Antigravity transcript, or "" if directory not found
func newestTranscript() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	// build a path pattern that matches any session directory
	pattern := filepath.Join(home, ".gemini", "antigravity-cli", "brain", "*", ".system_generated", "logs", "transcript.jsonl")
	
	// find transcript files matching the pattern
	matches, _ := filepath.Glob(pattern)

	var newest string
	var newestTime time.Time

	// compare modification times to find the newest transcript 
	for _, m := range matches {
		if info, err := os.Stat(m); err == nil && info.ModTime().After(newestTime) {
			newest, newestTime = m, info.ModTime()
		}
	}
	return newest
}

// start the transcript reader when the desktop appp launches with go routine
func (a *App) startup(ctx context.Context) {
	//get the active session info from environment variables
	a.conversationID = os.Getenv("LINUX_MENTOR_CONVERSATION_ID")
	a.transcriptPath = os.Getenv("LINUX_MENTOR_TRANSCRIPT_PATH")

	// follow the newest transcript when no specific path given
	a.followNewest = a.transcriptPath == ""

	// run the transcript reader concurrently with the desktop ui
	go func() {
		// check for new transcript data 400 ms
		ticker := time.NewTicker(400 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				// stop reading when the app shuts down
				return
			case <-ticker.C:
				// read new transcript data and log any errors
				if err := a.readTranscript(); err != nil {
					log.Printf("linux-mentor: read transcript: %v", err)
				}
			}
		}
	}() 
}

// return the active conversation id to the frontend
func (a *App) GetSessionInfo() SessionInfo {
	// lock the app state for reading while retrieving the id
	a.mu.RLock()
	defer a.mu.RUnlock()

	return SessionInfo{ConversationID: a.conversationID}
}

// return a copy of the mentor's messages for the frontend
func (a *App) GetMessages() []MentorMessage {
	// lock messages for reading while the copy is made
	a.mu.RLock()
	defer a.mu.RUnlock()

	// create a new slice and copy the stored messages into it
	messages := make([]MentorMessage, len(a.messages))
	copy(messages, a.messages)
	return messages
}

// progress information for one linux-related concept displayed in the ui
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

// get the saved learning progress and organize it for the desktop ui
func (a *App) GetLearningProgress() LearningProgress {
	
	// initalize empty lists so the ui has valid data even without saved progress
	result := LearningProgress{
		DueForReview:  []ReviewProgressItem{},
		Mastered:      []ReviewProgressItem{},
		AreasToWorkOn: []ReviewProgressItem{},
		AllItems:      []ReviewProgressItem{},
	}

	// locate the linux mentor state directory using the xdg path or home directory
	stateDir := os.Getenv("XDG_STATE_HOME")		
	if stateDir == "" {
		home, _ := os.UserHomeDir()
		stateDir = filepath.Join(home, ".local", "state")
	}
	// read the saved student progress data from progress.json
	data, err := os.ReadFile(filepath.Join(stateDir, "linux-mentor", "progress.json"))
	if err != nil {
		return result
	}

	// define the structure used to decode each saved concept from json
	type rawItem struct {
		Concept          string    `json:"concept"`
		Stage            int       `json:"stage"`	// 
		LastReviewed     time.Time `json:"last_reviewed"`
		NextReview       time.Time `json:"next_review"`
		CommandAccuracy  string    `json:"command_accuracy,omitempty"`
		ExplanationDepth string    `json:"explanation_depth,omitempty"`
		Notes            string    `json:"notes,omitempty"`
		Status           string    `json:"status"`
	}

	// declare map where key = concept name and value = pointers to rawItem structs
	var raw map[string]*rawItem	
	// decode the json bytes into map
	if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
		return result
	}

	// keys are sorted for consistent ui ordering
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// group results by status; before correct answer, 0 wrong is mastered, 1 wrong is needs review, >=2 wrong is needs reinforcement
	for _, key := range keys {
		item := raw[key]
		if item == nil {
			continue
		}
		status := item.Status
		if status == "" { // progress saved before status exists
			switch {
			case item.Stage >= 3:
				status = "mastered"
			case item.Stage <= 0:
				status = "reinforce"
			default:
				status = "review"
			}
		}

		entry := ReviewProgressItem{
			Concept:          key,		// use map key since it is the concept name
			Stage:            item.Stage,
			LastReviewed:     formatDay(item.LastReviewed),
			NextReview:       formatDay(item.NextReview),
			CommandAccuracy:  item.CommandAccuracy,
			ExplanationDepth: item.ExplanationDepth,
			Notes:            item.Notes,
		}
		result.AllItems = append(result.AllItems, entry)

		// group concepts by status and check whether reviews are due
		switch status {
		case "mastered":
			result.Mastered = append(result.Mastered, entry)
		case "reinforce":
			result.AreasToWorkOn = append(result.AreasToWorkOn, entry)
		case "review":
			if !time.Now().Before(item.NextReview) {
				result.DueForReview = append(result.DueForReview, entry)
			}
		}
	}
	return result
}

// read new transcript records and update the mentor messages
func (a *App) readTranscript() error {
	// lock shared app state while reading and updating transcript 
	a.mu.Lock()
	defer a.mu.Unlock()

	// switch to the newest transcript if no specific session was provided
	if a.followNewest {
		if newest := newestTranscript(); newest != "" && newest != a.transcriptPath {
			a.transcriptPath = newest

			// extract the conversation id from the transcript directory
			a.conversationID = filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(newest))))
			
			// reset the reader state when switching sessions
			a.offset = 0
			a.pending = nil
			a.fileInfo = nil
			a.messages = nil
		}
	}

	// check whether the transcript exists and get its file information 
	info, err := os.Stat(a.transcriptPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	// reset the reader if the transcript was replaced or truncated
	if a.fileInfo == nil || !os.SameFile(a.fileInfo, info) || info.Size() < a.offset {
		a.offset = 0
		a.pending = nil
		a.fileInfo = info
		a.messages = nil
	}
	a.fileInfo = info	// save the current file information

	file, err := os.Open(a.transcriptPath)
	if err != nil {
		return err
	}
	defer file.Close()

	// continue reading from the last saved byte position
	if _, err := file.Seek(a.offset, io.SeekStart); err != nil {
		return err
	}

	// read the transcript one line at a time
	reader := bufio.NewReader(file)
	for {
		line, readErr := reader.ReadBytes('\n')

		// update the byte position and keep any incomplete  data
		a.offset += int64(len(line))
		a.pending = append(a.pending, line...)

		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return readErr
		}

		// stop at the current end of the file and keep partial data for later
		if errors.Is(readErr, io.EOF) {
			return nil
		}

		// convert the completed lineinto a string and clear the pending buffer
		recordLine := strings.TrimSpace(string(a.pending))
		a.pending = a.pending[:0]

		if recordLine == "" {
			continue
		}
		// process the transcript record and return any parsing errors
		if err := a.appendAssistantMessage([]byte(recordLine)); err != nil {
			return err
		}
	}
}

// decode a transcript record and save the completed assistant responses
func (a *App) appendAssistantMessage(line []byte) error {
	// decode the json record into a transcriptRecord struct
	var record transcriptRecord
	if err := json.Unmarshal(line, &record); err != nil {
		return err
	}

	// ignore anything that is not a completed model response
	if record.Type != "PLANNER_RESPONSE" || record.Source != "MODEL" || record.Status != "DONE" {
		return nil
	}

	content := strings.TrimSpace(record.Content)
	if content == "" {
		return nil
	}

	// create a message and add it to the list displayed by the frontend
	a.messages = append(a.messages, MentorMessage{
		ID:        record.CreatedAt + "-" + time.Now().Format(time.RFC3339Nano),
		Content:   content,
		CreatedAt: record.CreatedAt,
	})
	return nil
}
