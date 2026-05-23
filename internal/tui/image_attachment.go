package tui

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pockyHM/conan/internal/fileref"
	"github.com/pockyHM/conan/internal/llm"
	"golang.design/x/clipboard"
)

const defaultVisionMaxTokens = 1200

type imageAttachment struct {
	ID        int
	Name      string
	Path      string
	MediaType string
	Hash      string
	Size      int64
	Width     int
	Height    int
}

type clipboardPasteMsg struct {
	image imageAttachment
	text  string
	err   error
}

var (
	clipboardInitOnce sync.Once
	clipboardInitErr  error
)

func clipboardImageOrTextCmd(dir string, nextID int) tea.Cmd {
	return func() tea.Msg {
		clipboardInitOnce.Do(func() {
			clipboardInitErr = clipboard.Init()
		})
		if clipboardInitErr != nil {
			return clipboardPasteMsg{err: clipboardInitErr}
		}
		if data := clipboard.Read(clipboard.FmtImage); len(data) > 0 {
			image, err := saveImageAttachment(data, "clipboard.png", "image/png", dir, nextID)
			if err != nil {
				return clipboardPasteMsg{err: err}
			}
			return clipboardPasteMsg{image: image}
		}
		if text := string(clipboard.Read(clipboard.FmtText)); text != "" {
			return clipboardPasteMsg{text: text}
		}
		return clipboardPasteMsg{}
	}
}

func saveImageAttachment(data []byte, name string, mediaType string, dir string, id int) (imageAttachment, error) {
	if len(data) == 0 {
		return imageAttachment{}, fmt.Errorf("image data is empty")
	}
	if mediaType == "" {
		mediaType = http.DetectContentType(data)
	}
	if !supportedImageMediaType(mediaType) {
		return imageAttachment{}, fmt.Errorf("unsupported image type: %s", mediaType)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return imageAttachment{}, fmt.Errorf("decode image metadata: %w", err)
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "conan-attachments")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return imageAttachment{}, err
	}
	ext := imageExt(mediaType)
	path := filepath.Join(dir, hash[:16]+ext)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return imageAttachment{}, err
	}
	base := strings.TrimSpace(filepath.Base(name))
	if base == "" || base == "." {
		base = "image" + ext
	}
	return imageAttachment{
		ID:        id,
		Name:      base,
		Path:      path,
		MediaType: mediaType,
		Hash:      hash,
		Size:      int64(len(data)),
		Width:     cfg.Width,
		Height:    cfg.Height,
	}, nil
}

func imageAttachmentsFromPastedText(text string, dir string, startID int) ([]imageAttachment, bool, error) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return nil, false, nil
	}
	attachments := make([]imageAttachment, 0, len(fields))
	for i, field := range fields {
		path := strings.Trim(field, `"'`)
		path = strings.TrimPrefix(path, "file://")
		if path == "" {
			return nil, false, nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, false, nil
		}
		mediaType := http.DetectContentType(data)
		if !supportedImageMediaType(mediaType) {
			return nil, false, nil
		}
		attachment, err := saveImageAttachment(data, filepath.Base(path), mediaType, dir, startID+i)
		if err != nil {
			return nil, false, err
		}
		attachments = append(attachments, attachment)
	}
	return attachments, true, nil
}

func imageAttachmentsFromFileRefs(root string, refs []fileref.Reference, dir string, startID int) ([]imageAttachment, []fileref.Reference, error) {
	if len(refs) == 0 {
		return nil, nil, nil
	}
	images := make([]imageAttachment, 0)
	textRefs := make([]fileref.Reference, 0, len(refs))
	nextID := startID
	for _, ref := range refs {
		path, err := resolveWorkspaceRefPath(root, ref.Path)
		if err != nil {
			return nil, nil, err
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			textRefs = append(textRefs, ref)
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			textRefs = append(textRefs, ref)
			continue
		}
		mediaType := http.DetectContentType(data)
		if !supportedImageMediaType(mediaType) {
			textRefs = append(textRefs, ref)
			continue
		}
		attachment, err := saveImageAttachment(data, filepath.Base(path), mediaType, dir, nextID)
		if err != nil {
			return nil, nil, err
		}
		images = append(images, attachment)
		nextID++
	}
	return images, textRefs, nil
}

func resolveWorkspaceRefPath(root string, raw string) (string, error) {
	if root == "" {
		root = "."
	}
	clean := filepath.Clean(strings.TrimSpace(raw))
	if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("file reference outside workspace: %s", raw)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	targetAbs, err := filepath.Abs(filepath.Join(rootAbs, clean))
	if err != nil {
		return "", err
	}
	if targetAbs != rootAbs && !strings.HasPrefix(targetAbs, rootAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("file reference outside workspace: %s", raw)
	}
	return targetAbs, nil
}

func supportedImageMediaType(mediaType string) bool {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "image/png", "image/jpeg", "image/gif":
		return true
	default:
		return false
	}
}

func imageExt(mediaType string) string {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}

func imageInputFromAttachment(att imageAttachment) (llm.ImageInput, error) {
	data, err := os.ReadFile(att.Path)
	if err != nil {
		return llm.ImageInput{}, err
	}
	return llm.ImageInput{Name: att.Name, MediaType: att.MediaType, Data: data}, nil
}

func imageChipText(images []imageAttachment) string {
	if len(images) == 0 {
		return ""
	}
	parts := make([]string, 0, len(images))
	for _, image := range images {
		parts = append(parts, fmt.Sprintf("[Image #%d]", image.ID))
	}
	return strings.Join(parts, " ")
}

func appendImageToolContext(input string, images []imageAttachment) string {
	if len(images) == 0 {
		return input
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(input))
	b.WriteString("\n\nAttached images are available to the model only through the image_analyze tool. Do not infer visual details without calling image_analyze first.\n")
	for _, image := range images {
		fmt.Fprintf(&b, "- [Image #%d] name=%q type=%s size=%dx%d bytes=%d\n", image.ID, image.Name, image.MediaType, image.Width, image.Height, image.Size)
	}
	return strings.TrimSpace(b.String())
}

func selectImageAttachments(images []imageAttachment, args imageAnalyzeArgs) ([]imageAttachment, error) {
	if len(images) == 0 {
		return nil, fmt.Errorf("no attached images are available")
	}
	wanted := make([]int, 0)
	if args.ImageID > 0 {
		wanted = append(wanted, args.ImageID)
	}
	for _, id := range args.ImageIDs {
		if id > 0 {
			wanted = append(wanted, id)
		}
	}
	if len(wanted) == 0 {
		return images, nil
	}
	byID := make(map[int]imageAttachment, len(images))
	for _, image := range images {
		byID[image.ID] = image
	}
	selected := make([]imageAttachment, 0, len(wanted))
	seen := make(map[int]bool)
	for _, id := range wanted {
		if seen[id] {
			continue
		}
		image, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("image #%d is not attached", id)
		}
		selected = append(selected, image)
		seen[id] = true
	}
	return selected, nil
}

func imageAnalyzePrompt(question string, images []imageAttachment, maxChars int) string {
	if maxChars <= 0 {
		maxChars = 1200
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Analyze the attached image(s) for a downstream text-only agent. Answer the user's question using only visible evidence. Keep the result concise, factual, and at most about %d characters per image.\n", maxChars)
	question = strings.TrimSpace(question)
	if question != "" {
		fmt.Fprintf(&b, "\nQuestion: %s\n", question)
	}
	b.WriteString("\nImages:\n")
	for _, image := range images {
		fmt.Fprintf(&b, "- Image #%d: %s (%s, %dx%d)\n", image.ID, image.Name, image.MediaType, image.Width, image.Height)
	}
	return strings.TrimSpace(b.String())
}
