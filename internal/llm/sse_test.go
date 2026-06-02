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

func TestReadSSEDebugLogsEventSummaryOnly(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "conan.jsonl")
	if err := logging.Setup(logging.Config{Level: "debug", File: logFile}); err != nil {
		t.Fatalf("setup logging: %v", err)
	}
	defer logging.Close()

	input := "event: completion\ndata: {\"choices\":[{\"delta\":{\"content\":\"secret chunk\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
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
		"data_len",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("debug log missing %q:\n%s", want, logText)
		}
	}
	for _, unwanted := range []string{"secret chunk", "finish_reason", "[DONE]"} {
		if strings.Contains(logText, unwanted) {
			t.Fatalf("debug log should not contain raw SSE data %q:\n%s", unwanted, logText)
		}
	}
}

func TestReadSSEDebugLogsRedactPasswordFields(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		secrets []string
	}{
		{
			name:    "embedded_json_string",
			input:   "event: completion\ndata: {\"tool_calls\":[{\"function\":{\"arguments\":\"{\\\"host\\\":\\\"10.0.0.5\\\",\\\"password\\\":\\\"secret\\\"}\"}}]}\n\n",
			secrets: []string{"secret"},
		},
		{
			name:    "escaped_backslash",
			input:   "event: completion\ndata: " + `{\"password\":\"alpha\\\\omega\"}` + "\n\n",
			secrets: []string{"alpha", "omega"},
		},
		{
			name:    "escaped_quote",
			input:   "event: completion\ndata: " + `{\"password\":\"quote\\\"leak\"}` + "\n\n",
			secrets: []string{"quote", "leak"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logFile := filepath.Join(t.TempDir(), "conan.jsonl")
			if err := logging.Setup(logging.Config{Level: "debug", File: logFile}); err != nil {
				t.Fatalf("setup logging: %v", err)
			}
			defer logging.Close()

			events := ReadSSE(strings.NewReader(tt.input))
			for event := range events {
				for _, secret := range tt.secrets {
					if !strings.Contains(event.Data, secret) {
						t.Fatalf("raw event data should preserve %q: %s", secret, event.Data)
					}
				}
			}
			logging.Close()

			contents, err := os.ReadFile(logFile)
			if err != nil {
				t.Fatalf("read log file: %v", err)
			}
			logText := string(contents)
			for _, secret := range tt.secrets {
				if strings.Contains(logText, secret) {
					t.Fatalf("debug log should not contain raw password fragment %q:\n%s", secret, logText)
				}
			}
			if !strings.Contains(logText, "data_len") {
				t.Fatalf("debug log should contain SSE data length:\n%s", logText)
			}
		})
	}
}

func TestReadSSEDebugLogsRedactToolArgumentFragments(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "conan.jsonl")
	if err := logging.Setup(logging.Config{Level: "debug", File: logFile}); err != nil {
		t.Fatalf("setup logging: %v", err)
	}
	defer logging.Close()

	input := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"password\":\"sup"}}]},"finish_reason":null}]}`
	input += "\n\n"
	input += "event: content_block_delta\n"
	input += `data: {"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"er-secret\"}"}}`
	input += "\n\n"
	events := ReadSSE(strings.NewReader(input))
	var raw []SSEEvent
	for event := range events {
		raw = append(raw, event)
	}
	if len(raw) != 2 {
		t.Fatalf("events = %d, want 2", len(raw))
	}
	if !strings.Contains(raw[0].Data, "sup") || !strings.Contains(raw[1].Data, "er-secret") {
		t.Fatalf("raw event data should be unchanged: %#v", raw)
	}
	logging.Close()

	contents, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	logText := string(contents)
	for _, secret := range []string{"sup", "er-secret"} {
		if strings.Contains(logText, secret) {
			t.Fatalf("debug log should not contain tool argument fragment %q:\n%s", secret, logText)
		}
	}
	if !strings.Contains(logText, "data_len") {
		t.Fatalf("debug log should contain SSE data length:\n%s", logText)
	}
}
