package sticker

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SecHex generates a random hex string of n bytes (2n hex characters).
func SecHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand unavailable: %v", err))
	}
	return hex.EncodeToString(b)
}

// ArchiveExtract extracts an archive file using bsdtar and returns
// a list of all extracted file paths.
func ArchiveExtract(f string) ([]string, error) {
	targetDir := filepath.Join(filepath.Dir(f), SecHex(4))
	if err := os.MkdirAll(targetDir, 0750); err != nil {
		return nil, fmt.Errorf("ArchiveExtract: failed to create target dir: %w", err)
	}

	out, err := exec.Command(bsdtarBin, "-xvf", f, "-C", targetDir).CombinedOutput()
	if err != nil {
		slog.Warn("ArchiveExtract failed", "error", err, "output", string(out))
		return nil, fmt.Errorf("ArchiveExtract: bsdtar failed: %w", err)
	}

	files := LsFilesR(targetDir, nil, nil)
	return files, nil
}

// LsFilesR recursively lists files under dir. Files are included only if
// their path contains ALL mustHave keywords (case-insensitive) and NONE
// of the mustNotHave keywords.
func LsFilesR(dir string, mustHave, mustNotHave []string) []string {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if matchFilter(path, mustHave, mustNotHave) {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		slog.Warn("LsFilesR walk error", "dir", dir, "error", err)
		return nil
	}
	slog.Debug("LsFilesR result", "dir", dir, "count", len(files))
	return files
}

// LsFiles lists files in a single directory (non-recursive). Files are
// included only if their path contains ALL mustHave keywords and NONE
// of the mustNotHave keywords (case-insensitive).
func LsFiles(dir string, mustHave, mustNotHave []string) []string {
	var files []string
	entries, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		slog.Warn("LsFiles glob error", "dir", dir, "error", err)
		return nil
	}

	for _, p := range entries {
		info, err := os.Stat(p)
		if err != nil {
			slog.Debug("LsFiles stat error", "path", p, "error", err)
			continue
		}
		if info.IsDir() {
			continue
		}
		if matchFilter(p, mustHave, mustNotHave) {
			files = append(files, p)
		}
	}
	return files
}

// matchFilter returns true if path contains all mustHave keywords
// and none of the mustNotHave keywords (case-insensitive).
func matchFilter(path string, mustHave, mustNotHave []string) bool {
	lower := strings.ToLower(path)

	for _, kw := range mustHave {
		if !strings.Contains(lower, strings.ToLower(kw)) {
			return false
		}
	}
	for _, kw := range mustNotHave {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return false
		}
	}
	return true
}

// FCompress creates a ZIP archive at path f containing the given files.
// It strips 2 path components from each file path in the archive.
func FCompress(f string, flist []string) error {
	args := []string{"--strip-components", "2", "-avcf", f}
	args = append(args, flist...)

	slog.Debug("FCompress", "output", f, "files", len(flist))
	out, err := exec.Command(bsdtarBin, args...).CombinedOutput()
	if err != nil {
		slog.Error("FCompress failed", "error", err, "output", string(out))
		return fmt.Errorf("FCompress: bsdtar failed: %w", err)
	}
	return nil
}

// FCompressVol creates one or more ZIP archives, splitting files into
// volumes of up to 50MB each. Returns the list of created ZIP file paths.
func FCompressVol(f string, flist []string) ([]string, error) {
	const maxVolSize int64 = 50 * MB

	basename := filepath.Base(f)
	dir := filepath.Dir(f)

	var volumes [][]string
	var curSize int64

	for _, file := range flist {
		st, err := os.Stat(file)
		if err != nil {
			slog.Debug("FCompressVol: skipping file", "file", file, "error", err)
			continue
		}

		fSize := st.Size()
		// Start a new volume if empty or if adding this file would exceed the limit.
		if len(volumes) == 0 || (curSize+fSize >= maxVolSize && curSize > 0) {
			volumes = append(volumes, nil)
			curSize = 0
		}
		volumes[len(volumes)-1] = append(volumes[len(volumes)-1], file)
		curSize += fSize
	}

	var zipPaths []string
	for i, files := range volumes {
		var zipName string
		if len(volumes) == 1 {
			zipName = basename
		} else {
			zipName = strings.TrimSuffix(basename, ".zip") + fmt.Sprintf("_00%d.zip", i+1)
		}

		zipPath := filepath.Join(dir, zipName)
		if err := FCompress(zipPath, files); err != nil {
			return zipPaths, fmt.Errorf("FCompressVol: failed on volume %d: %w", i+1, err)
		}
		zipPaths = append(zipPaths, zipPath)
	}
	return zipPaths, nil
}
