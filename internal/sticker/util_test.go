package sticker

import (
	"archive/zip"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestFCompressStoresOnlyBaseNames(t *testing.T) {
	dir := t.TempDir()
	firstDir := filepath.Join(dir, "first", "nested")
	secondDir := filepath.Join(dir, "second", "nested")
	if err := os.MkdirAll(firstDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(secondDir, 0750); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(firstDir, "one.png")
	second := filepath.Join(secondDir, "two.png")
	if err := os.WriteFile(first, []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("two"), 0600); err != nil {
		t.Fatal(err)
	}

	archivePath := filepath.Join(dir, "stickers.zip")
	if err := FCompress(archivePath, []string{first, second}); err != nil {
		t.Fatalf("FCompress: %v", err)
	}

	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer zr.Close()
	names := make([]string, 0, len(zr.File))
	for _, file := range zr.File {
		names = append(names, file.Name)
	}
	sort.Strings(names)
	if want := []string{"one.png", "two.png"}; len(names) != len(want) || names[0] != want[0] || names[1] != want[1] {
		t.Fatalf("ZIP member names = %q, want %q", names, want)
	}
}

func TestFCompressRejectsDuplicateBaseNames(t *testing.T) {
	dir := t.TempDir()
	firstDir := filepath.Join(dir, "first")
	secondDir := filepath.Join(dir, "second")
	if err := os.MkdirAll(firstDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(secondDir, 0750); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(firstDir, "sticker.png")
	second := filepath.Join(secondDir, "sticker.png")
	if err := os.WriteFile(first, []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("two"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := FCompress(filepath.Join(dir, "stickers.zip"), []string{first, second}); err == nil {
		t.Fatal("FCompress accepted duplicate archive member names")
	}
}
