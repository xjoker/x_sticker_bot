package storage

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// TempManager manages temporary directories for user operations.
type TempManager struct {
	baseDir string
}

// NewTempManager creates a TempManager and ensures baseDir exists.
func NewTempManager(baseDir string) (*TempManager, error) {
	abs, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve base dir: %w", err)
	}

	if err := os.MkdirAll(abs, 0750); err != nil {
		return nil, fmt.Errorf("storage: create base dir %q: %w", abs, err)
	}

	return &TempManager{baseDir: abs}, nil
}

// CreateUserDir creates a temporary directory for the given user ID.
// The directory name is {uid}_{randomHex}/ under baseDir.
func (tm *TempManager) CreateUserDir(uid int64) (string, error) {
	randBytes := make([]byte, 8)
	if _, err := rand.Read(randBytes); err != nil {
		return "", fmt.Errorf("storage: generate random hex: %w", err)
	}

	dirName := fmt.Sprintf("%d_%s", uid, hex.EncodeToString(randBytes))
	dirPath := filepath.Join(tm.baseDir, dirName)

	if err := os.Mkdir(dirPath, 0750); err != nil {
		return "", fmt.Errorf("storage: create user dir %q: %w", dirPath, err)
	}

	slog.Debug("created user temp dir", "uid", uid, "path", dirPath)
	return dirPath, nil
}

// CleanUserDir removes the specified directory. It validates that the path
// is within baseDir before removing.
func (tm *TempManager) CleanUserDir(dir string) error {
	if _, err := tm.SafePath(dir); err != nil {
		return fmt.Errorf("storage: clean user dir: %w", err)
	}

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("storage: remove dir %q: %w", dir, err)
	}

	slog.Debug("cleaned user temp dir", "path", dir)
	return nil
}

// PurgeOutdated removes directories under baseDir that are older than maxAge.
// Only directories (not files) are removed. Returns the number of purged directories.
func (tm *TempManager) PurgeOutdated(maxAge time.Duration) (int, error) {
	entries, err := os.ReadDir(tm.baseDir)
	if err != nil {
		return 0, fmt.Errorf("storage: read base dir: %w", err)
	}

	cutoff := time.Now().Add(-maxAge)
	purged := 0

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		fullPath := filepath.Join(tm.baseDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			slog.Warn("failed to get dir info", "path", fullPath, "error", err)
			continue
		}

		if info.ModTime().Before(cutoff) {
			if err := os.RemoveAll(fullPath); err != nil {
				slog.Warn("failed to purge temp dir", "path", fullPath, "error", err)
				continue
			}
			purged++
			slog.Debug("purged outdated temp dir", "path", fullPath, "mod_time", info.ModTime())
		}
	}

	if purged > 0 {
		slog.Info("purged outdated temp directories", "count", purged)
	}
	return purged, nil
}

// SafePath validates that the given path resolves to a location within baseDir.
// It returns the cleaned absolute path if valid, or an error if the path
// escapes baseDir (path traversal prevention).
func (tm *TempManager) SafePath(userInput string) (string, error) {
	absPath, err := filepath.Abs(userInput)
	if err != nil {
		return "", fmt.Errorf("storage: resolve path: %w", err)
	}

	rel, err := filepath.Rel(tm.baseDir, absPath)
	if err != nil {
		return "", fmt.Errorf("storage: compute relative path: %w", err)
	}

	// If the relative path starts with "..", it escapes baseDir.
	if rel == ".." || len(rel) >= 3 && rel[:3] == "../" {
		return "", fmt.Errorf("storage: path %q is outside base dir %q", userInput, tm.baseDir)
	}

	return absPath, nil
}

// StartAutoClean runs PurgeOutdated at the given interval in a background goroutine.
// It stops when the stop channel is closed.
func (tm *TempManager) StartAutoClean(interval, maxAge time.Duration, stop <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		slog.Info("temp auto-clean started", "interval", interval, "max_age", maxAge)

		for {
			select {
			case <-stop:
				slog.Info("temp auto-clean stopped")
				return
			case <-ticker.C:
				count, err := tm.PurgeOutdated(maxAge)
				if err != nil {
					slog.Error("auto-clean purge failed", "error", err)
				} else if count > 0 {
					slog.Info("auto-clean purged directories", "count", count)
				}
			}
		}
	}()
}
