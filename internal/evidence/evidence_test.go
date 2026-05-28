package evidence

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRecorderAppendsAndSortsEvents(t *testing.T) {
	rec := NewRecorder("prod", []string{"web-1"}, fixedClock(time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)))
	incident, err := rec.Start("API latency")
	if err != nil {
		t.Fatal(err)
	}

	rec.Append(Event{Source: SourceTool, Timestamp: time.Date(2026, 5, 23, 10, 2, 0, 0, time.UTC), ToolName: "svc_status", Summary: "service ok"})
	rec.Append(Event{Source: SourceUser, Timestamp: time.Date(2026, 5, 23, 10, 1, 0, 0, time.UTC), Summary: "check api"})
	events := rec.Events()

	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].Source != SourceUser || events[1].ToolName != "svc_status" {
		t.Fatalf("events not sorted: %#v", events)
	}
	if events[0].IncidentID != incident.ID {
		t.Fatalf("incident id = %q, want %q", events[0].IncidentID, incident.ID)
	}
	if events[0].Cluster != "prod" || strings.Join(events[0].Nodes, ",") != "web-1" {
		t.Fatalf("event scope missing: %#v", events[0])
	}
}

func TestRecorderTruncatesLongSummary(t *testing.T) {
	rec := NewRecorder("prod", nil, fixedClock(time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)))
	if _, err := rec.Start("Long output"); err != nil {
		t.Fatal(err)
	}

	rec.Append(Event{Source: SourceTool, Summary: strings.Repeat("界", 1300)})

	events := rec.Events()
	if got := len([]rune(events[0].Summary)); got != maxSummaryRunes {
		t.Fatalf("summary runes = %d, want %d", got, maxSummaryRunes)
	}
}

func TestRecorderRejectsSecretLikeEvent(t *testing.T) {
	rec := NewRecorder("prod", nil, fixedClock(time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)))
	if _, err := rec.Start("Secret"); err != nil {
		t.Fatal(err)
	}

	rec.Append(Event{Source: SourceTool, Summary: "password=secret"})

	if len(rec.Events()) != 0 {
		t.Fatalf("secret-like event should be rejected: %#v", rec.Events())
	}
}

func TestRecorderAcceptsRedactedSecretArguments(t *testing.T) {
	rec := NewRecorder("prod", nil, fixedClock(time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)))
	if _, err := rec.Start("Node add"); err != nil {
		t.Fatal(err)
	}

	rec.Append(Event{Source: SourceTool, Arguments: json.RawMessage(`{"password":"[REDACTED]"}`), Summary: "node_add requested"})

	if len(rec.Events()) != 1 {
		t.Fatalf("redacted arguments should be accepted: %#v", rec.Events())
	}
}

func TestRecorderAppendWithoutOpenIncidentNoops(t *testing.T) {
	rec := NewRecorder("prod", nil, fixedClock(time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)))

	rec.Append(Event{Source: SourceUser, Summary: "hello"})

	if len(rec.Events()) != 0 {
		t.Fatalf("append without incident should no-op: %#v", rec.Events())
	}
}

func TestRecorderNoteAndClose(t *testing.T) {
	rec := NewRecorder("prod", []string{"web-1"}, fixedClock(time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)))
	if _, err := rec.Start("API latency"); err != nil {
		t.Fatal(err)
	}

	rec.Note("checked nginx logs")
	closed, err := rec.Close("incidents/report.md")
	if err != nil {
		t.Fatal(err)
	}

	if closed.Status != StatusClosed || closed.Report != "incidents/report.md" {
		t.Fatalf("closed incident = %#v", closed)
	}
	if rec.Current() != nil {
		t.Fatal("current incident should be nil after close")
	}
	events := rec.Events()
	if len(events) != 1 || events[0].Source != SourceUser || events[0].Summary != "checked nginx logs" {
		t.Fatalf("note event = %#v", events)
	}
}

func TestEventArgumentsAreCopied(t *testing.T) {
	rec := NewRecorder("prod", nil, fixedClock(time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)))
	if _, err := rec.Start("Args"); err != nil {
		t.Fatal(err)
	}
	args := json.RawMessage(`{"command":"uptime"}`)
	rec.Append(Event{Source: SourceTool, Arguments: args, Summary: "ok"})
	args[12] = 'X'

	if string(rec.Events()[0].Arguments) != `{"command":"uptime"}` {
		t.Fatalf("arguments were not copied: %s", rec.Events()[0].Arguments)
	}
}

func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}
