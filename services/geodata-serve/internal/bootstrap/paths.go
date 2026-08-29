package bootstrap

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// PathOptions are the explicit filesystem roots accepted by the service.
type PathOptions struct {
	Database   string
	RuntimeDir string
	BackupDir  string
	WorkingDir string
}

// Paths contains cleaned absolute paths used by a running service.
type Paths struct {
	Database   string
	RuntimeDir string
	BackupDir  string
	WorkingDir string
}

func ResolvePaths(options PathOptions) (Paths, error) {
	if options.Database == "" || options.RuntimeDir == "" || options.BackupDir == "" || options.WorkingDir == "" {
		return Paths{}, errors.New("database, runtime-dir, backup-dir, and working-dir are required")
	}

	database, err := absoluteClean(options.Database)
	if err != nil {
		return Paths{}, fmt.Errorf("database path: %w", err)
	}
	runtimeDir, err := absoluteClean(options.RuntimeDir)
	if err != nil {
		return Paths{}, fmt.Errorf("runtime-dir path: %w", err)
	}
	backupDir, err := absoluteClean(options.BackupDir)
	if err != nil {
		return Paths{}, fmt.Errorf("backup-dir path: %w", err)
	}
	workingDir, err := absoluteClean(options.WorkingDir)
	if err != nil {
		return Paths{}, fmt.Errorf("working-dir path: %w", err)
	}

	info, err := os.Stat(workingDir)
	if err != nil {
		return Paths{}, fmt.Errorf("working-dir: %w", err)
	}
	if !info.IsDir() {
		return Paths{}, errors.New("working-dir is not a directory")
	}

	if info, err := os.Lstat(database); err == nil && info.IsDir() {
		return Paths{}, errors.New("database path is a directory")
	} else if err == nil && info.Mode()&os.ModeSymlink != 0 {
		return Paths{}, errors.New("database path is a symlink")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Paths{}, fmt.Errorf("database: %w", err)
	}
	for name, path := range map[string]string{"runtime-dir": runtimeDir, "backup-dir": backupDir} {
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return Paths{}, fmt.Errorf("%s is a symlink", name)
		} else if err == nil && !info.IsDir() {
			return Paths{}, fmt.Errorf("%s is not a directory", name)
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return Paths{}, fmt.Errorf("%s: %w", name, err)
		}
	}

	return Paths{
		Database:   database,
		RuntimeDir: runtimeDir,
		BackupDir:  backupDir,
		WorkingDir: workingDir,
	}, nil
}

func (p Paths) ExtensionsDir() string { return filepath.Join(p.RuntimeDir, "extensions") }

func (p Paths) DuckDBTempDir() string { return filepath.Join(p.RuntimeDir, "duckdb-tmp") }

func (p Paths) ServerStateFile() string { return filepath.Join(p.RuntimeDir, "server.json") }

// EnsureDirectories creates only service-owned directory roots and the database parent.
func (p Paths) EnsureDirectories() error {
	for name, path := range map[string]string{
		"runtime-dir": p.RuntimeDir,
		"backup-dir":  p.BackupDir,
		"extensions":  p.ExtensionsDir(),
		"duckdb-tmp":  p.DuckDBTempDir(),
	} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", name, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(p.Database), 0o700); err != nil {
		return fmt.Errorf("create database parent: %w", err)
	}
	return nil
}

func absoluteClean(path string) (string, error) {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}
