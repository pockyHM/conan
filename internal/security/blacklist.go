package security

import (
	"regexp"
	"strings"
)

type Blacklist struct {
	patterns []*regexp.Regexp
}

func NewBlacklist(entries []string) Blacklist {
	patterns := make([]*regexp.Regexp, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		pattern, err := regexp.Compile(entry)
		if err != nil {
			continue
		}
		patterns = append(patterns, pattern)
	}
	return Blacklist{patterns: patterns}
}

func (b Blacklist) Match(command string) bool {
	command = strings.TrimSpace(command)
	for _, pattern := range b.patterns {
		if pattern.MatchString(command) {
			return true
		}
	}
	return false
}
