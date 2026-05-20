package models

import "github.com/google/uuid"

func NewID() string {
	return uuid.New().String()[:8]
}

const (
	CategoryEvent           = "event"
	CategoryExperience      = "experience"
	CategoryTroubleshooting = "troubleshooting"
	CategoryTopology        = "topology"
)

type Memory struct {
	ID         string `json:"id"`
	Category   string `json:"category"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	Tags       string `json:"tags"`
	SourceConv string `json:"source_conv"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

type Conversation struct {
	ID        string `json:"id"`
	Cluster   string `json:"cluster"`
	Nodes     string `json:"nodes"`
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Summary   string `json:"summary"`
	Messages  string `json:"messages"`
}

type Message struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	Role           string `json:"role"`
	Content        string `json:"content"`
	ToolCallID     string `json:"tool_call_id,omitempty"`
	ToolName       string `json:"tool_name,omitempty"`
	ToolInput      string `json:"tool_input,omitempty"`
	ToolOutput     string `json:"tool_output,omitempty"`
	CreatedAt      string `json:"created_at"`
}

type AuditEntry struct {
	ID        string `json:"id"`
	Node      string `json:"node"`
	ToolName  string `json:"tool_name"`
	Input     string `json:"input"`
	RiskLevel string `json:"risk_level"`
	CreatedAt string `json:"created_at"`
}

type NodeStatus struct {
	Name   string  `json:"name"`
	Host   string  `json:"host"`
	Online bool    `json:"online"`
	CPU    float64 `json:"cpu_percent"`
	Mem    float64 `json:"mem_percent"`
	Load1  float64 `json:"load_1"`
	Load5  float64 `json:"load_5"`
	Load15 float64 `json:"load_15"`
}
