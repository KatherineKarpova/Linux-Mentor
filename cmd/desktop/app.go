package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
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

func (a *App) startup(ctx context.Context) {
	a.conversationID = os.Getenv("LINUX_MENTOR_CONVERSATION_ID")
	a.transcriptPath = os.Getenv("LINUX_MENTOR_TRANSCRIPT_PATH")

	if a.transcriptPath == "" {
		return
	}

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

func (a *App) readTranscript() error {
	a.mu.Lock()
	defer a.mu.Unlock()

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
