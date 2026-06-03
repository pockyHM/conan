package subagent

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTranscriptWritesJSONL(t *testing.T) {
	dir := t.TempDir()
	tr, err := OpenTranscript(dir, "sess-1", "id-1")
	if err != nil {
		t.Fatalf("OpenTranscript: %v", err)
	}

	events := []Event{
		{ID: "id-1", Kind: EventTurnStart, Turn: 1},
		{ID: "id-1", Kind: EventToolCall, Turn: 1, Tool: "tool_search", Args: `{"q":"x"}`},
		{ID: "id-1", Kind: EventDone, Turn: 1},
	}
	for _, ev := range events {
		if err := tr.Write(ev); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	path := filepath.Join(dir, "logs", "subagents", "sess-1", "id-1.jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open transcript: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}

	var first Event
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if first.Kind != EventTurnStart || first.Turn != 1 {
		t.Errorf("first event = %#v, want TurnStart turn=1", first)
	}
}

func TestOpenTranscriptCreatesNestedDirs(t *testing.T) {
	dir := t.TempDir()
	tr, err := OpenTranscript(dir, "deep/sess", "id-2")
	if err != nil {
		t.Fatalf("OpenTranscript: %v", err)
	}
	defer tr.Close()

	expected := filepath.Join(dir, "logs", "subagents", "deep", "sess", "id-2.jsonl")
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("expected file %s to exist: %v", expected, err)
	}
}
