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

// RuntimeLayout contains the absolute paths owned by the service runtime.
// All derived runtime paths are computed from RuntimeDir here so callers do
// not need to duplicate the service's private directory layout.
type RuntimeLayout struct {
	RuntimeDir      string
	ExtensionsDir   string
	DuckDBTempDir   string
	ServerStateFile string
}

// ResolveRuntimeLayout resolves the service-owned runtime directory and its
// derived paths. It does not create any directories.
func ResolveRuntimeLayout(runtimeDir string) (RuntimeLayout, error) {
	if runtimeDir == "" {
		return RuntimeLayout{}, errors.New("runtime-dir is required")
	}
	runtimeDir, err := absoluteClean(runtimeDir)
	if err != nil {
		return RuntimeLayout{}, fmt.Errorf("runtime-dir path: %w", err)
	}
	if info, err := os.Lstat(runtimeDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return RuntimeLayout{}, errors.New("runtime-dir is a symlink")
		}
		if !info.IsDir() {
			return RuntimeLayout{}, errors.New("runtime-dir is not a directory")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return RuntimeLayout{}, fmt.Errorf("runtime-dir: %w", err)
	}
	return RuntimeLayout{
		RuntimeDir:      runtimeDir,
		ExtensionsDir:   filepath.Join(runtimeDir, "extensions"),
		DuckDBTempDir:   filepath.Join(runtimeDir, "duckdb-tmp"),
		ServerStateFile: filepath.Join(runtimeDir, "server.json"),
	}, nil
}

// EnsureDirectories creates only the service-owned runtime directories.
func (l RuntimeLayout) EnsureDirectories() error {
	for name, path := range map[string]string{
		"runtime-dir": l.RuntimeDir,
		"extensions":  l.ExtensionsDir,
		"duckdb-tmp":  l.DuckDBTempDir,
	} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", name, err)
		}
	}
	return nil
}

// Paths contains cleaned absolute paths used by a running service.
type Paths struct {
	Database   string
	RuntimeDir string
	BackupDir  string
	WorkingDir string
	RuntimeLayout
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
	layout, err := ResolveRuntimeLayout(runtimeDir)
	if err != nil {
		return Paths{}, err
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
		Database:      database,
		RuntimeDir:    runtimeDir,
		BackupDir:     backupDir,
		WorkingDir:    workingDir,
		RuntimeLayout: layout,
	}, nil
}

func (p Paths) ExtensionsDir() string {
	return p.RuntimeLayout.ExtensionsDir
}

func (p Paths) DuckDBTempDir() string {
	return p.RuntimeLayout.DuckDBTempDir
}

func (p Paths) ServerStateFile() string {
	return p.RuntimeLayout.ServerStateFile
}

// EnsureDirectories creates only service-owned directory roots and the database parent.
func (p Paths) EnsureDirectories() error {
	if err := p.RuntimeLayout.EnsureDirectories(); err != nil {
		return err
	}
	if err := os.MkdirAll(p.BackupDir, 0o700); err != nil {
		return fmt.Errorf("create backup-dir: %w", err)
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
