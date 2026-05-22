package llm

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pockyHM/conan/internal/logging"
)

func TestReadSSEParsesEventsWithData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "event: message_start\ndata: {\"type\":\"message_start\"}\n\n")
		flusher.Flush()
		fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\"}\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	events := ReadSSE(resp.Body)
	var collected []SSEEvent
	for e := range events {
		collected = append(collected, e)
	}
	if len(collected) != 2 {
		t.Fatalf("events = %d, want 2", len(collected))
	}
	if collected[0].Event != "message_start" || collected[0].Data != `{"type":"message_start"}` {
		t.Fatalf("event[0] = %+v", collected[0])
	}
	if collected[1].Event != "content_block_delta" {
		t.Fatalf("event[1].Event = %q", collected[1].Event)
	}
}

func TestReadSSEHandlesDataOnlyLines(t *testing.T) {
	input := "data: hello\n\ndata: world\n\n"
	events := ReadSSE(strings.NewReader(input))
	var collected []SSEEvent
	for e := range events {
		collected = append(collected, e)
	}
	if len(collected) != 2 {
		t.Fatalf("events = %d, want 2", len(collected))
	}
	if collected[0].Data != "hello" {
		t.Fatalf("data[0] = %q", collected[0].Data)
	}
	if collected[1].Data != "world" {
		t.Fatalf("data[1] = %q", collected[1].Data)
	}
}

func TestReadSSESendsDoneSentinel(t *testing.T) {
	input := "data: first\n\ndata: [DONE]\n\n"
	events := ReadSSE(strings.NewReader(input))
	var collected []SSEEvent
	for e := range events {
		collected = append(collected, e)
	}
	if len(collected) != 2 {
		t.Fatalf("events = %d, want 2", len(collected))
	}
	if collected[1].Data != "[DONE]" {
		t.Fatalf("data[1] = %q", collected[1].Data)
	}
}

func TestReadSSEDebugLogsRawEvents(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "conan.jsonl")
	if err := logging.Setup(logging.Config{Level: "debug", File: logFile}); err != nil {
		t.Fatalf("setup logging: %v", err)
	}
	defer logging.Close()

	input := "event: completion\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	events := ReadSSE(strings.NewReader(input))
	for range events {
	}
	logging.Close()

	contents, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	logText := string(contents)
	for _, want := range []string{
		"llm raw sse event",
		"completion",
		"finish_reason",
		"stop",
		"[DONE]",
		"data_len",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("debug log missing %q:\n%s", want, logText)
		}
	}
}

func TestReadSSEDebugLogsRedactPasswordFields(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "conan.jsonl")
	if err := logging.Setup(logging.Config{Level: "debug", File: logFile}); err != nil {
		t.Fatalf("setup logging: %v", err)
	}
	defer logging.Close()

	input := "event: completion\ndata: {\"tool_calls\":[{\"function\":{\"arguments\":\"{\\\"host\\\":\\\"10.0.0.5\\\",\\\"password\\\":\\\"secret\\\"}\"}}]}\n\n"
	events := ReadSSE(strings.NewReader(input))
	for range events {
	}
	logging.Close()

	contents, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	logText := string(contents)
	if strings.Contains(logText, "secret") {
		t.Fatalf("debug log should not contain raw password:\n%s", logText)
	}
	if !strings.Contains(logText, "[redacted]") {
		t.Fatalf("debug log should contain redacted marker:\n%s", logText)
	}
}
