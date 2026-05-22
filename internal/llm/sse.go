package llm

import (
	"bufio"
	"io"
	"log/slog"
	"strings"
)

func sanitizeSSELogData(data string) string {
	var b strings.Builder
	last := 0
	for i := 0; i < len(data); i++ {
		keyEnd, ok := matchSSEPasswordKey(data, i)
		if !ok {
			continue
		}
		j := skipSSELogSpace(data, keyEnd)
		if j >= len(data) || data[j] != ':' {
			continue
		}
		j = skipSSELogSpace(data, j+1)
		valueStart, valueEnd, closeEnd, ok := findSSELogStringValue(data, j)
		if !ok {
			continue
		}
		if b.Cap() == 0 {
			b.Grow(len(data))
		}
		b.WriteString(data[last:valueStart])
		b.WriteString("[redacted]")
		last = valueEnd
		i = closeEnd - 1
	}
	if b.Cap() == 0 {
		return data
	}
	b.WriteString(data[last:])
	return b.String()
}

func matchSSEPasswordKey(data string, i int) (int, bool) {
	const rawKey = `"password"`
	const escapedKey = `\"password\"`
	if i+len(rawKey) <= len(data) && strings.EqualFold(data[i:i+len(rawKey)], rawKey) {
		return i + len(rawKey), true
	}
	if i+len(escapedKey) <= len(data) && strings.EqualFold(data[i:i+len(escapedKey)], escapedKey) {
		return i + len(escapedKey), true
	}
	return 0, false
}

func skipSSELogSpace(data string, i int) int {
	for i < len(data) {
		switch data[i] {
		case ' ', '\t', '\r', '\n':
			i++
		default:
			return i
		}
	}
	return i
}

func findSSELogStringValue(data string, i int) (valueStart int, valueEnd int, closeEnd int, ok bool) {
	if i >= len(data) {
		return 0, 0, 0, false
	}
	if data[i] == '"' {
		if close := findRawSSELogStringClose(data, i+1); close >= 0 {
			return i + 1, close, close + 1, true
		}
		return 0, 0, 0, false
	}
	if i+1 < len(data) && data[i] == '\\' && data[i+1] == '"' {
		if close := findEscapedSSELogStringClose(data, i+2); close >= 0 {
			return i + 2, close, close + 2, true
		}
	}
	return 0, 0, 0, false
}

func findRawSSELogStringClose(data string, start int) int {
	for i := start; i < len(data); i++ {
		if data[i] == '"' && countBackslashesBefore(data, i)%2 == 0 {
			return i
		}
	}
	return -1
}

func findEscapedSSELogStringClose(data string, start int) int {
	for i := start; i+1 < len(data); i++ {
		if data[i] == '\\' && data[i+1] == '"' {
			run := countBackslashesEndingAt(data, i)
			if run%4 == 1 {
				return i
			}
			i++
		}
	}
	return -1
}

func countBackslashesBefore(data string, i int) int {
	count := 0
	for j := i - 1; j >= 0 && data[j] == '\\'; j-- {
		count++
	}
	return count
}

func countBackslashesEndingAt(data string, i int) int {
	count := 0
	for j := i; j >= 0 && data[j] == '\\'; j-- {
		count++
	}
	return count
}

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
					ev := SSEEvent{Event: event.String(), Data: data.String()}
					slog.Debug("llm raw sse event", "event", ev.Event, "data", sanitizeSSELogData(ev.Data), "data_len", len(ev.Data))
					ch <- ev
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
