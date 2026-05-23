package fileguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateTextFileAllowsUTF8Text(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(path, []byte("hello\n世界\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := ValidateTextFile(path); err != nil {
		t.Fatalf("ValidateTextFile: %v", err)
	}
}

func TestValidateTextFileRejectsImagesAndBinary(t *testing.T) {
	dir := t.TempDir()
	imagePath := filepath.Join(dir, "image.png")
	if err := os.WriteFile(imagePath, []byte("not really png"), 0644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	if err := ValidateTextFile(imagePath); err == nil || !strings.Contains(err.Error(), "binary/image") {
		t.Fatalf("image error = %v, want binary/image rejection", err)
	}

	binaryPath := filepath.Join(dir, "payload.txt")
	if err := os.WriteFile(binaryPath, []byte{'o', 'k', 0, 'x'}, 0644); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	if err := ValidateTextFile(binaryPath); err == nil || !strings.Contains(err.Error(), "binary content") {
		t.Fatalf("binary error = %v, want binary content rejection", err)
	}
}

func TestValidateTextContentRejectsNUL(t *testing.T) {
	if err := ValidateTextContent("abc\x00def"); err == nil {
		t.Fatal("expected NUL content to be rejected")
	}
}

func TestLimitTextOutputCapsBytes(t *testing.T) {
	out, truncated := LimitTextOutput("abcdef", 3)
	if !truncated {
		t.Fatal("expected output to be truncated")
	}
	if !strings.HasPrefix(out, "abc\n[truncated:") {
		t.Fatalf("output = %q", out)
	}
}

func TestReplaceLineRange(t *testing.T) {
	got, err := ReplaceLineRange("one\ntwo\nthree\n", 2, 3, "TWO\nTHREE")
	if err != nil {
		t.Fatalf("ReplaceLineRange: %v", err)
	}
	if got != "one\nTWO\nTHREE\n" {
		t.Fatalf("updated = %q", got)
	}
}
