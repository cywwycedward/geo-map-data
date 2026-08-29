package bootstrap

import (
	"path/filepath"
	"testing"
)

func TestResolvePathsMakesAbsoluteAndCleansInputs(t *testing.T) {
	t.Parallel()
	workingDir := t.TempDir()

	paths, err := ResolvePaths(PathOptions{
		Database:   filepath.Join(workingDir, "..", "data", "main.duckdb"),
		RuntimeDir: filepath.Join(workingDir, ".", "runtime", "..", "runtime"),
		BackupDir:  filepath.Join(workingDir, "backups", "..", "backups"),
		WorkingDir: filepath.Join(workingDir, "."),
	})
	if err != nil {
		t.Fatalf("ResolvePaths() error = %v", err)
	}

	for name, got := range map[string]string{
		"database": paths.Database,
		"runtime":  paths.RuntimeDir,
		"backup":   paths.BackupDir,
		"working":  paths.WorkingDir,
	} {
		if !filepath.IsAbs(got) {
			t.Errorf("%s path %q is not absolute", name, got)
		}
		if got != filepath.Clean(got) {
			t.Errorf("%s path %q is not clean", name, got)
		}
	}
}

func TestResolvePathsRejectsMissingWorkingDirectory(t *testing.T) {
	_, err := ResolvePaths(PathOptions{
		Database:   filepath.Join(t.TempDir(), "main.duckdb"),
		RuntimeDir: filepath.Join(t.TempDir(), "runtime"),
		BackupDir:  filepath.Join(t.TempDir(), "backups"),
		WorkingDir: filepath.Join(t.TempDir(), "missing"),
	})
	if err == nil {
		t.Fatal("ResolvePaths() error = nil, want missing working directory error")
	}
}
