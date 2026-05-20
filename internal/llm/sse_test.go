package llm

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
