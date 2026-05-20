package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db  *sql.DB
	dir string
}

type MemoryEntry struct {
	ID         string `json:"id"`
	Category   string `json:"category"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	Tags       string `json:"tags"`
	SourceConv string `json:"source_conv"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

type ConversationRecord struct {
	ID        string `json:"id"`
	Cluster   string `json:"cluster"`
	Nodes     string `json:"nodes"`
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Summary   string `json:"summary"`
	Messages  string `json:"messages"`
}

type ConversationSummary struct {
	ID        string `json:"id"`
	Cluster   string `json:"cluster"`
	CreatedAt string `json:"created_at"`
	Summary   string `json:"summary"`
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create memory dir: %w", err)
	}
	dbPath := filepath.Join(dir, "conan.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	s := &Store{db: db, dir: dir}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Dir() string {
	return s.dir
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS memories (
			id          TEXT PRIMARY KEY,
			category    TEXT,
			title       TEXT,
			content     TEXT,
			tags        TEXT,
			source_conv TEXT,
			created_at  TEXT,
			updated_at  TEXT
		);
		CREATE TABLE IF NOT EXISTS conversations (
			id          TEXT PRIMARY KEY,
			cluster     TEXT,
			nodes       TEXT,
			model       TEXT,
			created_at  TEXT,
			updated_at  TEXT,
			summary     TEXT,
			messages    TEXT
		);
	`)
	return err
}

func (s *Store) SaveMemory(entry MemoryEntry) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if entry.CreatedAt == "" {
		entry.CreatedAt = now
	}
	entry.UpdatedAt = now
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO memories (id, category, title, content, tags, source_conv, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.Category, entry.Title, entry.Content, entry.Tags, entry.SourceConv, entry.CreatedAt, entry.UpdatedAt,
	)
	return err
}

func (s *Store) UpdateMemory(entry MemoryEntry) error {
	entry.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`UPDATE memories SET category=?, title=?, content=?, tags=?, updated_at=? WHERE id=?`,
		entry.Category, entry.Title, entry.Content, entry.Tags, entry.UpdatedAt, entry.ID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("memory not found: %s", entry.ID)
	}
	return nil
}

func (s *Store) DeleteMemory(id string) error {
	res, err := s.db.Exec(`DELETE FROM memories WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("memory not found: %s", id)
	}
	return nil
}

func (s *Store) GetMemory(id string) (*MemoryEntry, error) {
	row := s.db.QueryRow(
		`SELECT id, category, title, content, tags, source_conv, created_at, updated_at FROM memories WHERE id=?`, id,
	)
	var e MemoryEntry
	if err := row.Scan(&e.ID, &e.Category, &e.Title, &e.Content, &e.Tags, &e.SourceConv, &e.CreatedAt, &e.UpdatedAt); err != nil {
		return nil, err
	}
	return &e, nil
}

func (s *Store) SearchMemories(query string, limit int) ([]MemoryEntry, error) {
	if limit <= 0 {
		limit = 10
	}
	pattern := "%" + query + "%"
	rows, err := s.db.Query(
		`SELECT id, category, title, content, tags, source_conv, created_at, updated_at
		 FROM memories
		 WHERE title LIKE ? OR content LIKE ? OR tags LIKE ? OR category LIKE ?
		 ORDER BY updated_at DESC
		 LIMIT ?`,
		pattern, pattern, pattern, pattern, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []MemoryEntry
	for rows.Next() {
		var e MemoryEntry
		if err := rows.Scan(&e.ID, &e.Category, &e.Title, &e.Content, &e.Tags, &e.SourceConv, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		results = append(results, e)
	}
	return results, nil
}

func (s *Store) ListMemories(category string, limit int) ([]MemoryEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	var rows *sql.Rows
	var err error
	if category != "" {
		rows, err = s.db.Query(
			`SELECT id, category, title, content, tags, source_conv, created_at, updated_at
			 FROM memories WHERE category=? ORDER BY updated_at DESC LIMIT ?`, category, limit,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT id, category, title, content, tags, source_conv, created_at, updated_at
			 FROM memories ORDER BY updated_at DESC LIMIT ?`, limit,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []MemoryEntry
	for rows.Next() {
		var e MemoryEntry
		if err := rows.Scan(&e.ID, &e.Category, &e.Title, &e.Content, &e.Tags, &e.SourceConv, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		results = append(results, e)
	}
	return results, nil
}

func (s *Store) SaveConversation(rec ConversationRecord) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if rec.CreatedAt == "" {
		rec.CreatedAt = now
	}
	rec.UpdatedAt = now
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO conversations (id, cluster, nodes, model, created_at, updated_at, summary, messages)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.Cluster, rec.Nodes, rec.Model, rec.CreatedAt, rec.UpdatedAt, rec.Summary, rec.Messages,
	)
	return err
}

func (s *Store) ListConversations(limit int) ([]ConversationSummary, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(
		`SELECT id, cluster, created_at, summary FROM conversations ORDER BY updated_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []ConversationSummary
	for rows.Next() {
		var r ConversationSummary
		if err := rows.Scan(&r.ID, &r.Cluster, &r.CreatedAt, &r.Summary); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, nil
}

func (s *Store) LoadConversation(id string) (*ConversationRecord, error) {
	row := s.db.QueryRow(
		`SELECT id, cluster, nodes, model, created_at, updated_at, summary, messages FROM conversations WHERE id=?`, id,
	)
	var r ConversationRecord
	if err := row.Scan(&r.ID, &r.Cluster, &r.Nodes, &r.Model, &r.CreatedAt, &r.UpdatedAt, &r.Summary, &r.Messages); err != nil {
		return nil, err
	}
	return &r, nil
}

func marshalTags(tags []string) string {
	b, _ := json.Marshal(tags)
	return string(b)
}

func unmarshalTags(s string) []string {
	var tags []string
	json.Unmarshal([]byte(s), &tags)
	return tags
}
