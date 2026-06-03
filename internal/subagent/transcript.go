package subagent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Transcript struct {
	path string
	file *os.File
	enc  *json.Encoder
	mu   sync.Mutex
}

func OpenTranscript(configHome, sessionID, id string) (*Transcript, error) {
	dir := filepath.Join(configHome, "logs", "subagents", sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create transcript dir: %w", err)
	}
	path := filepath.Join(dir, id+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open transcript file: %w", err)
	}
	return &Transcript{path: path, file: f, enc: json.NewEncoder(f)}, nil
}

func (t *Transcript) Write(ev Event) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.enc.Encode(ev)
}

func (t *Transcript) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.file == nil {
		return nil
	}
	err := t.file.Close()
	t.file = nil
	return err
}

func (t *Transcript) Path() string {
	return t.path
}
