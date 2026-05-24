package evidence

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/pockyHM/conan/pkg/models"
)

const (
	StatusOpen      = "open"
	StatusClosed    = "closed"
	maxSummaryRunes = 1200
)

type Source string

const (
	SourceUser          Source = "user"
	SourceAssistant     Source = "assistant"
	SourceTool          Source = "tool"
	SourceObservability Source = "observability"
	SourceSubagent      Source = "subagent"
	SourceMemory        Source = "memory"
	SourceRisk          Source = "risk"
)

type Incident struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Cluster   string    `json:"cluster,omitempty"`
	Nodes     []string  `json:"nodes,omitempty"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"started_at"`
	ClosedAt  time.Time `json:"closed_at,omitempty"`
	Report    string    `json:"report,omitempty"`
}

type Event struct {
	ID          string            `json:"id"`
	IncidentID  string            `json:"incident_id"`
	Source      Source            `json:"source"`
	Cluster     string            `json:"cluster,omitempty"`
	Nodes       []string          `json:"nodes,omitempty"`
	Service     string            `json:"service,omitempty"`
	Timestamp   time.Time         `json:"timestamp"`
	ToolName    string            `json:"tool_name,omitempty"`
	Arguments   json.RawMessage   `json:"arguments,omitempty"`
	Summary     string            `json:"summary"`
	RawRef      string            `json:"raw_ref,omitempty"`
	RiskLevel   string            `json:"risk_level,omitempty"`
	RiskOutcome string            `json:"risk_outcome,omitempty"`
	Success     *bool             `json:"success,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type Recorder struct {
	cluster   string
	nodes     []string
	now       func() time.Time
	current   *Incident
	events    []Event
	incidents []Incident
}

func NewRecorder(cluster string, nodes []string, now func() time.Time) *Recorder {
	if now == nil {
		now = time.Now
	}
	return &Recorder{cluster: cluster, nodes: append([]string(nil), nodes...), now: now}
}

func (r *Recorder) Start(title string) (Incident, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Incident{}, fmt.Errorf("incident title is required")
	}
	if r.current != nil {
		return *r.current, fmt.Errorf("incident already open")
	}
	incident := Incident{
		ID:        models.NewID(),
		Title:     title,
		Cluster:   r.cluster,
		Nodes:     append([]string(nil), r.nodes...),
		Status:    StatusOpen,
		StartedAt: r.now(),
	}
	r.current = &incident
	r.incidents = append(r.incidents, incident)
	return incident, nil
}

func (r *Recorder) Current() *Incident {
	if r.current == nil {
		return nil
	}
	cp := *r.current
	cp.Nodes = append([]string(nil), r.current.Nodes...)
	return &cp
}

func (r *Recorder) Append(event Event) {
	if r.current == nil {
		return
	}
	event.Summary = truncateRunes(strings.TrimSpace(event.Summary), maxSummaryRunes)
	if event.Summary == "" || containsSecretLike(event.Summary) || containsSecretLikeArguments(event.Arguments) {
		return
	}
	if event.ID == "" {
		event.ID = models.NewID()
	}
	event.IncidentID = r.current.ID
	if event.Cluster == "" {
		event.Cluster = r.current.Cluster
	}
	if len(event.Nodes) == 0 {
		event.Nodes = append([]string(nil), r.current.Nodes...)
	} else {
		event.Nodes = append([]string(nil), event.Nodes...)
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = r.now()
	}
	if len(event.Arguments) > 0 {
		event.Arguments = append(json.RawMessage(nil), event.Arguments...)
	}
	if event.Metadata != nil {
		cp := make(map[string]string, len(event.Metadata))
		for key, value := range event.Metadata {
			cp[key] = value
		}
		event.Metadata = cp
	}
	r.events = append(r.events, event)
}

func (r *Recorder) Events() []Event {
	events := append([]Event(nil), r.events...)
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})
	for i := range events {
		events[i].Nodes = append([]string(nil), events[i].Nodes...)
		events[i].Arguments = append(json.RawMessage(nil), events[i].Arguments...)
	}
	return events
}

func (r *Recorder) Note(content string) {
	r.Append(Event{Source: SourceUser, Summary: content})
}

func (r *Recorder) Close(report string) (Incident, error) {
	if r.current == nil {
		return Incident{}, fmt.Errorf("no open incident")
	}
	r.current.Status = StatusClosed
	r.current.ClosedAt = r.now()
	r.current.Report = report
	closed := *r.current
	r.current = nil
	for i := range r.incidents {
		if r.incidents[i].ID == closed.ID {
			r.incidents[i] = closed
			break
		}
	}
	return closed, nil
}

func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

func containsSecretLike(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{
		"token",
		"password",
		"passwd",
		"pwd=",
		"secret",
		"api key",
		"api_key",
		"apikey",
		"private key",
		"bearer ",
		"authorization:",
		"authorization=",
		"-----begin private key-----",
		"-----begin rsa private key-----",
		"-----begin openssh private key-----",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func containsSecretLikeArguments(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return containsSecretLike(string(raw))
	}
	return containsSecretLikeJSONValue("", value)
}

func containsSecretLikeJSONValue(key string, value any) bool {
	if isSecretKey(key) && !isRedactedValue(value) {
		return true
	}
	switch v := value.(type) {
	case map[string]any:
		for childKey, childValue := range v {
			if containsSecretLikeJSONValue(childKey, childValue) {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if containsSecretLikeJSONValue("", item) {
				return true
			}
		}
	case string:
		if isRedactedValue(v) {
			return false
		}
		return containsSecretLike(v)
	}
	return false
}

func isSecretKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	switch key {
	case "token", "password", "passwd", "secret", "api_key", "apikey", "private_key", "authorization":
		return true
	default:
		return false
	}
}

func isRedactedValue(value any) bool {
	if value == nil {
		return false
	}
	text, ok := value.(string)
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "[redacted]", "redacted", "***", "xxxxx", "*****":
		return true
	default:
		return false
	}
}
