package security

import "strings"

type Whitelist struct {
	entries []string
}

func NewWhitelist(entries []string) Whitelist {
	cleaned := make([]string, 0, len(entries))
	for _, e := range entries {
		trimmed := strings.TrimSpace(e)
		if trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	return Whitelist{entries: cleaned}
}

func (w Whitelist) Match(command string) bool {
	command = strings.TrimSpace(command)
	for _, entry := range w.entries {
		if command == entry || strings.HasPrefix(command, entry+" ") {
			return true
		}
	}
	return false
}
