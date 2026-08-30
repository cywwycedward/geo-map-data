package bootstrap

import (
	"errors"
	"os"
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

func TestResolvePathsUsesOneCanonicalRuntimeLayout(t *testing.T) {
	workingDir := t.TempDir()
	paths, err := ResolvePaths(PathOptions{
		Database:   filepath.Join(workingDir, "..", "data", "main.duckdb"),
		RuntimeDir: filepath.Join(workingDir, "runtime", ".", "nested", ".."),
		BackupDir:  filepath.Join(workingDir, "backups", "."),
		WorkingDir: workingDir,
	})
	if err != nil {
		t.Fatalf("ResolvePaths() error = %v", err)
	}
	if paths.ExtensionsDir() != paths.RuntimeLayout.ExtensionsDir || paths.DuckDBTempDir() != paths.RuntimeLayout.DuckDBTempDir || paths.ServerStateFile() != paths.RuntimeLayout.ServerStateFile {
		t.Fatalf("path accessors do not use canonical layout: %#v", paths)
	}
	if err := paths.EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories() error = %v", err)
	}
	for name, path := range map[string]string{
		"runtime":    paths.RuntimeLayout.RuntimeDir,
		"extensions": paths.RuntimeLayout.ExtensionsDir,
		"duckdb-tmp": paths.RuntimeLayout.DuckDBTempDir,
		"backups":    paths.BackupDir,
	} {
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			t.Errorf("%s directory = %q, stat error = %v", name, path, err)
		}
	}
	for _, path := range []string{filepath.Join(paths.RuntimeLayout.RuntimeDir, "DATA.md"), filepath.Join(paths.RuntimeLayout.RuntimeDir, "sql")} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("EnsureDirectories() created external path %q: %v", path, err)
		}
	}
}
