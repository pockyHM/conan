package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Config struct {
	Level string
	File  string
}

var (
	mu          sync.Mutex
	currentFile *os.File
)

func Setup(cfg Config) error {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return err
	}

	writer := io.Discard
	var newFile *os.File
	if cfg.File != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.File), 0o755); err != nil {
			return err
		}
		file, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		newFile = file
		writer = file
	}

	logger := slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: level}))

	mu.Lock()
	oldFile := currentFile
	currentFile = newFile
	slog.SetDefault(logger)
	mu.Unlock()

	if oldFile != nil {
		_ = oldFile.Close()
	}
	return nil
}

func Close() {
	mu.Lock()
	defer mu.Unlock()

	if currentFile != nil {
		_ = currentFile.Close()
		currentFile = nil
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(io.Discard, nil)))
}

func Write(msg string) {
	slog.Info(msg)
}

func parseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, nil
	}
}
