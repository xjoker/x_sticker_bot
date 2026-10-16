package sticker

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
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
// Archive members use only the source file base names.
func FCompress(f string, flist []string) (err error) {
	names := make(map[string]struct{}, len(flist))
	for _, file := range flist {
		name := filepath.Base(file)
		if _, exists := names[name]; exists {
			return fmt.Errorf("FCompress: duplicate archive member name %q", name)
		}
		names[name] = struct{}{}
	}

	output, err := os.Create(f)
	if err != nil {
		return fmt.Errorf("FCompress: create archive: %w", err)
	}
	defer func() {
		if closeErr := output.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("FCompress: close archive: %w", closeErr)
		}
		if err != nil {
			_ = os.Remove(f)
		}
	}()

	writer := zip.NewWriter(output)
	defer func() {
		if closeErr := writer.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("FCompress: finalize archive: %w", closeErr)
		}
	}()

	slog.Debug("FCompress", "output", f, "files", len(flist))
	for _, file := range flist {
		input, openErr := os.Open(file)
		if openErr != nil {
			return fmt.Errorf("FCompress: open %q: %w", file, openErr)
		}

		member, createErr := writer.Create(filepath.Base(file))
		if createErr == nil {
			_, createErr = io.Copy(member, input)
		}
		closeErr := input.Close()
		if createErr != nil {
			return fmt.Errorf("FCompress: write %q: %w", file, createErr)
		}
		if closeErr != nil {
			return fmt.Errorf("FCompress: close %q: %w", file, closeErr)
		}
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
