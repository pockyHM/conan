package fileguard

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	sampleBytes  = 8192
	MaxReadBytes = 64 * 1024
)

var blockedExtensions = map[string]bool{
	".7z":    true,
	".a":     true,
	".app":   true,
	".avif":  true,
	".bin":   true,
	".bmp":   true,
	".bz2":   true,
	".class": true,
	".dmg":   true,
	".dll":   true,
	".dylib": true,
	".exe":   true,
	".gif":   true,
	".gz":    true,
	".heic":  true,
	".ico":   true,
	".iso":   true,
	".jar":   true,
	".jpeg":  true,
	".jpg":   true,
	".mov":   true,
	".mp3":   true,
	".mp4":   true,
	".o":     true,
	".pdf":   true,
	".png":   true,
	".rar":   true,
	".so":    true,
	".svg":   true,
	".tar":   true,
	".tgz":   true,
	".tif":   true,
	".tiff":  true,
	".wasm":  true,
	".webp":  true,
	".xz":    true,
	".zip":   true,
}

func ValidateTextPath(path string) error {
	ext := strings.ToLower(filepath.Ext(path))
	if blockedExtensions[ext] {
		return fmt.Errorf("binary/image files are not allowed: %s", path)
	}
	return nil
}

func ValidateTextContent(content string) error {
	if !utf8.ValidString(content) || strings.ContainsRune(content, '\x00') {
		return fmt.Errorf("binary content is not allowed")
	}
	return nil
}

func LimitTextOutput(text string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || maxBytes > MaxReadBytes {
		maxBytes = MaxReadBytes
	}
	if len(text) <= maxBytes {
		return text, false
	}
	trimmed := trimTrailingPartialRune([]byte(text[:maxBytes]))
	return string(trimmed) + fmt.Sprintf("\n[truncated: output exceeds %d bytes]", maxBytes), true
}

func ReplaceLineRange(content string, startLine int, endLine int, replacement string) (string, error) {
	if startLine <= 0 {
		return "", fmt.Errorf("start_line must be >= 1")
	}
	if endLine == 0 {
		endLine = startLine
	}
	if endLine < startLine {
		return "", fmt.Errorf("end_line must be >= start_line")
	}

	newline := "\n"
	if strings.Contains(content, "\r\n") {
		newline = "\r\n"
		content = strings.ReplaceAll(content, "\r\n", "\n")
		replacement = strings.ReplaceAll(replacement, "\r\n", "\n")
	}
	hadFinalNewline := strings.HasSuffix(content, "\n")
	body := strings.TrimSuffix(content, "\n")
	lines := []string{}
	if body != "" {
		lines = strings.Split(body, "\n")
	}
	if startLine > len(lines)+1 {
		return "", fmt.Errorf("start_line %d exceeds file length %d", startLine, len(lines))
	}
	if endLine > len(lines) {
		return "", fmt.Errorf("end_line %d exceeds file length %d", endLine, len(lines))
	}

	replacement = strings.TrimSuffix(replacement, "\n")
	replacementLines := []string{}
	if replacement != "" {
		replacementLines = strings.Split(replacement, "\n")
	}
	start := startLine - 1
	end := endLine
	updated := make([]string, 0, len(lines)-max(0, end-start)+len(replacementLines))
	updated = append(updated, lines[:start]...)
	updated = append(updated, replacementLines...)
	updated = append(updated, lines[end:]...)

	out := strings.Join(updated, "\n")
	if hadFinalNewline {
		out += "\n"
	}
	if newline == "\r\n" {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}
	return out, nil
}

func EffectiveEndLine(startLine int, endLine int) int {
	if endLine == 0 {
		return startLine
	}
	return endLine
}

func ValidateTextFile(path string) error {
	if err := ValidateTextPath(path); err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	buf := make([]byte, sampleBytes)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return err
	}
	sample := trimTrailingPartialRune(buf[:n])
	if !utf8.Valid(sample) || hasNUL(sample) {
		return fmt.Errorf("binary content is not allowed: %s", path)
	}
	return nil
}

func trimTrailingPartialRune(data []byte) []byte {
	for trim := 0; trim < 4 && len(data) > 0 && !utf8.Valid(data); trim++ {
		data = data[:len(data)-1]
	}
	return data
}

func hasNUL(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}
