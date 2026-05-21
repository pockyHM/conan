package security

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const maxAuditInputLen = 200

// AuditEntry records a security decision or tool execution outcome.
type AuditEntry struct {
	Tool    string   `json:"tool"`
	Input   string   `json:"input"`
	Risk    string   `json:"risk"`
	Outcome string   `json:"outcome,omitempty"`
	Reason  string   `json:"reason,omitempty"`
	Nodes   []string `json:"nodes,omitempty"`
}

// AuditLogger appends human-readable audit entries to a log file.
type AuditLogger struct {
	file *os.File
	mu   sync.Mutex
}

// NewAuditLogger creates parent directories and opens path for append logging.
func NewAuditLogger(path string) (*AuditLogger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	return &AuditLogger{file: file}, nil
}

// Close closes the underlying audit log file.
func (l *AuditLogger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

// Log appends one audit line. Write errors are intentionally ignored because
// audit logging must not interrupt the interactive operations flow.
func (l *AuditLogger) Log(entry AuditEntry) {
	if l == nil {
		return
	}

	entry.Input = truncateAuditInput(entry.Input)
	jsonEntry, err := json.Marshal(entry)
	if err != nil {
		jsonEntry = []byte(`{"error":"marshal audit entry"}`)
	}

	parts := []string{
		time.Now().Format(time.RFC3339),
		fmt.Sprintf("[%s]", entry.Risk),
		"tool=" + entry.Tool,
		"nodes=" + strings.Join(entry.Nodes, ","),
		fmt.Sprintf("input=%q", entry.Input),
	}
	if entry.Reason != "" {
		parts = append(parts, fmt.Sprintf("reason=%q", entry.Reason))
	}
	if entry.Outcome != "" {
		parts = append(parts, fmt.Sprintf("outcome=%q", entry.Outcome))
	}
	parts = append(parts, string(jsonEntry))

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return
	}
	_, _ = l.file.WriteString(strings.Join(parts, " ") + "\n")
}

func truncateAuditInput(input string) string {
	if len(input) <= maxAuditInputLen {
		return input
	}
	return input[:maxAuditInputLen-3] + "..."
}
