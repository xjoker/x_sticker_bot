package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanUserDirRejectsBaseDirectory(t *testing.T) {
	base := t.TempDir()
	tm, err := NewTempManager(base)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(base, "stats.db")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := tm.CleanUserDir(base); err == nil {
		t.Error("cleaning base directory must fail")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("persistent data was deleted: %v", err)
	}
}

func TestCleanUserDirOnlyRemovesManagedDirectory(t *testing.T) {
	base := t.TempDir()
	tm, err := NewTempManager(base)
	if err != nil {
		t.Fatal(err)
	}
	managed, err := tm.CreateUserDir(42)
	if err != nil {
		t.Fatal(err)
	}
	if err := tm.CleanUserDir(managed); err != nil {
		t.Fatalf("clean managed directory: %v", err)
	}
	if _, err := os.Stat(managed); !os.IsNotExist(err) {
		t.Fatalf("managed directory remains: %v", err)
	}

	unmanaged := filepath.Join(base, "config")
	if err := os.Mkdir(unmanaged, 0750); err != nil {
		t.Fatal(err)
	}
	if err := tm.CleanUserDir(unmanaged); err == nil {
		t.Error("cleaning unmanaged directory must fail")
	}
	if _, err := os.Stat(unmanaged); err != nil {
		t.Fatalf("unmanaged directory was deleted: %v", err)
	}
}

func TestCleanUserDirDoesNotFollowManagedNameSymlink(t *testing.T) {
	base := t.TempDir()
	tm, err := NewTempManager(base)
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	marker := filepath.Join(target, "keep")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "42_0123456789abcdef")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := tm.CleanUserDir(link); err == nil {
		t.Error("cleaning managed-name symlink must fail")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("symlink target was deleted: %v", err)
	}
}

func TestPurgeOutdatedPreservesUnmanagedDirectories(t *testing.T) {
	base := t.TempDir()
	tm, err := NewTempManager(base)
	if err != nil {
		t.Fatal(err)
	}
	oldTask, err := tm.CreateUserDir(42)
	if err != nil {
		t.Fatal(err)
	}
	freshTask, err := tm.CreateUserDir(42)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(oldTask, old, old); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config", "backups", "42_invalid", "42_0123456789abcdeg"} {
		dir := filepath.Join(base, name)
		if err := os.Mkdir(dir, 0750); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(dir, old, old); err != nil {
			t.Fatal(err)
		}
	}
	n, err := tm.PurgeOutdated(24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("purged %d directories, want only 1 managed task", n)
	}
	if _, err := os.Stat(oldTask); !os.IsNotExist(err) {
		t.Errorf("expired task remains: %v", err)
	}
	for _, name := range []string{"config", "backups", "42_invalid", "42_0123456789abcdeg", filepath.Base(freshTask)} {
		if _, err := os.Stat(filepath.Join(base, name)); err != nil {
			t.Errorf("directory %s should remain: %v", name, err)
		}
	}
}
