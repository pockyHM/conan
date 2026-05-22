package memory

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"

	"github.com/pockyHM/conan/pkg/models"
)

const (
	maxMemoryTitleRunes   = 120
	maxMemorySectionRunes = 120
	maxMemoryContentRunes = 4000
	maxMemorySummaryRunes = 500
)

type MemoryCandidate struct {
	ID       string
	Category string
	Title    string
	Content  string
	Tags     []string
}

type MemoryDestination struct {
	Kind string
	Path string
}

func DestinationFor(candidate MemoryCandidate, cluster string) MemoryDestination {
	switch candidate.Category {
	case "profile":
		return MemoryDestination{Kind: "markdown", Path: "profile.md"}
	case "rule":
		return MemoryDestination{Kind: "markdown", Path: "rules/ops.md"}
	case "topology":
		if strings.TrimSpace(cluster) == "" {
			cluster = "default"
		}
		return MemoryDestination{Kind: "markdown", Path: "clusters/" + clusterFilename(cluster) + ".md"}
	case "runbook":
		return MemoryDestination{Kind: "markdown-note", Path: "runbooks"}
	case "incident":
		return MemoryDestination{Kind: "markdown-note", Path: "incidents"}
	case "event":
		return MemoryDestination{Kind: "sqlite"}
	default:
		return MemoryDestination{Kind: "discard"}
	}
}

func CandidateFromExplicitRemember(input string, cluster string) (MemoryCandidate, bool) {
	content, prefix, ok := explicitRememberContent(input)
	if !ok {
		return MemoryCandidate{}, false
	}
	category := "event"
	lower := strings.ToLower(content)
	switch {
	case strings.Contains(content, "我叫") || strings.Contains(lower, "my name is"):
		category = "profile"
	case explicitRememberPrefixIsRule(prefix) || strings.Contains(content, "规范") || strings.Contains(content, "必须") || strings.Contains(content, "以后") || strings.Contains(lower, "always"):
		category = "rule"
	case strings.Contains(content, "集群") || strings.Contains(content, "节点") || strings.Contains(content, "服务"):
		category = "topology"
	}
	return MemoryCandidate{
		ID:       models.NewID(),
		Category: category,
		Title:    MemoryTitle(content),
		Content:  content,
		Tags:     []string{"user", "explicit"},
	}, true
}

func ValidateMemoryCandidate(candidate MemoryCandidate, evidenceText string, requireEvidence bool) error {
	if !supportedMemoryCandidateCategory(candidate.Category) {
		return fmt.Errorf("unsupported memory category: %s", candidate.Category)
	}
	if err := validateMemoryTitle("title", candidate.Title); err != nil {
		return err
	}
	if err := validateMemoryContent("content", candidate.Content); err != nil {
		return err
	}
	if err := rejectSecretLikeMemoryText(candidate.Title + "\n" + candidate.Content); err != nil {
		return err
	}
	if requireEvidence && !memoryContentHasEvidence(candidate.Content, evidenceText) {
		return fmt.Errorf("memory content has no evidence in current turn")
	}
	return nil
}

func supportedMemoryCandidateCategory(category string) bool {
	switch strings.TrimSpace(category) {
	case "profile", "rule", "topology", "runbook", "incident", "event":
		return true
	default:
		return false
	}
}

func validateMemoryTitle(field string, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len([]rune(value)) > maxMemoryTitleRunes {
		return fmt.Errorf("%s exceeds %d characters", field, maxMemoryTitleRunes)
	}
	return nil
}

func validateMemorySection(field string, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len([]rune(value)) > maxMemorySectionRunes {
		return fmt.Errorf("%s exceeds %d characters", field, maxMemorySectionRunes)
	}
	return nil
}

func validateMemorySummary(field string, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len([]rune(value)) > maxMemorySummaryRunes {
		return fmt.Errorf("%s exceeds %d characters", field, maxMemorySummaryRunes)
	}
	return nil
}

func validateMemoryContent(field string, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len([]rune(value)) > maxMemoryContentRunes {
		return fmt.Errorf("%s exceeds %d characters", field, maxMemoryContentRunes)
	}
	return nil
}

func rejectSecretLikeMemoryText(text string) error {
	if containsSecretLikeMemoryText(text) {
		return fmt.Errorf("memory content appears to contain secret-like data")
	}
	return nil
}

func containsSecretLikeMemoryText(text string) bool {
	lower := strings.ToLower(text)
	secretMarkers := []string{
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
		"authorization =",
		"authorization=",
		"-----begin private key-----",
		"-----begin rsa private key-----",
		"-----begin openssh private key-----",
	}
	for _, marker := range secretMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func memoryContentHasEvidence(content string, evidenceText string) bool {
	needle := normalizeEvidenceText(content)
	if needle == "" {
		return false
	}
	return strings.Contains(normalizeEvidenceText(evidenceText), needle)
}

func normalizeEvidenceText(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

func ExtractExplicitRememberContent(input string) (string, bool) {
	content, _, ok := explicitRememberContent(input)
	return content, ok
}

func explicitRememberContent(input string) (string, string, bool) {
	text := strings.TrimSpace(input)
	lower := strings.ToLower(text)
	prefixes := []string{"请记住", "帮我记住", "记住", "记一下", "以后记得", "请记录", "记录一下", "remember that", "remember", "note that", "note:"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, strings.ToLower(prefix)) {
			content := strings.TrimSpace(text[len(prefix):])
			content = strings.TrimLeft(content, " ：:，,。.")
			return content, prefix, content != ""
		}
	}
	return "", "", false
}

func MemoryTitle(content string) string {
	line := strings.TrimSpace(strings.SplitN(content, "\n", 2)[0])
	if line == "" {
		return "User memory"
	}
	if len([]rune(line)) <= 48 {
		return line
	}
	runes := []rune(line)
	return string(runes[:45]) + "..."
}

func clusterFilename(cluster string) string {
	normalized := strings.ToLower(strings.TrimSpace(cluster))
	var b strings.Builder
	lastDash := false
	for _, r := range normalized {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	name := strings.Trim(b.String(), "-_")
	if name != "" {
		return name
	}
	sum := sha1.Sum([]byte(normalized))
	return "cluster-" + hex.EncodeToString(sum[:])[:12]
}

func explicitRememberPrefixIsRule(prefix string) bool {
	switch strings.ToLower(strings.TrimSpace(prefix)) {
	case "以后记得", "以后记住", "always remember", "from now on remember":
		return true
	default:
		return false
	}
}
