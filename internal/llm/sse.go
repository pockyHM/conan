package llm

import (
	"bufio"
	"io"
	"strings"
)

// SSEEvent represents a single Server-Sent Event.
type SSEEvent struct {
	Event string
	Data  string
}

// ReadSSE reads an io.Reader as an SSE stream, parsing event: and data: lines,
// and emits structured SSEEvent values on the returned channel.
// The channel is closed when the reader reaches EOF.
func ReadSSE(reader io.Reader) <-chan SSEEvent {
	ch := make(chan SSEEvent, 10)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(reader)
		var event, data strings.Builder
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if data.Len() > 0 {
					ch <- SSEEvent{Event: event.String(), Data: data.String()}
				}
				event.Reset()
				data.Reset()
				continue
			}
			if strings.HasPrefix(line, "event:") {
				event.WriteString(strings.TrimSpace(line[6:]))
			} else if strings.HasPrefix(line, "data:") {
				if data.Len() > 0 {
					data.WriteByte('\n')
				}
				val := line[5:]
				if len(val) > 0 && val[0] == ' ' {
					val = val[1:]
				}
				data.WriteString(val)
			}
		}
	}()
	return ch
}
