package restore

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"services/geodata-serve/internal/backup"
	"services/geodata-serve/internal/runtime"
)

func TestRestoreMissingBackupDoesNotChangeCurrentDatabase(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(root, "current.duckdb")
	if err := os.WriteFile(database, []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}
	options := Options{
		DatabasePath: database,
		RuntimeDir:   filepath.Join(root, "runtime"),
		BackupPath:   filepath.Join(root, "missing-backup"),
	}
	if err := Restore(context.Background(), options); err == nil {
		t.Fatal("Restore() error = nil, want missing backup error")
	}
	contents, err := os.ReadFile(database)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "current" {
		t.Fatalf("current database changed to %q", contents)
	}
}

func TestRestoreRejectsUnverifiedBackupWithoutChangingCurrentDatabase(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(root, "current.duckdb")
	if err := os.WriteFile(database, []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(root, "backup")
	if err := os.MkdirAll(backup, 0o700); err != nil {
		t.Fatal(err)
	}

	err := Restore(context.Background(), Options{
		DatabasePath: database,
		RuntimeDir:   filepath.Join(root, "runtime"),
		BackupPath:   backup,
	})
	if err == nil {
		t.Fatal("Restore() error = nil, want unverified backup error")
	}
	contents, err := os.ReadFile(database)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "current" {
		t.Fatalf("current database changed to %q", contents)
	}
}

func TestRestoreRejectsInvalidVerificationMarker(t *testing.T) {
	root := t.TempDir()
	backupPath := filepath.Join(root, "backup")
	if err := os.MkdirAll(backupPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupPath, backup.VerifiedMarker), []byte("not verified\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Restore(context.Background(), Options{
		DatabasePath: filepath.Join(root, "current.duckdb"),
		RuntimeDir:   filepath.Join(root, "runtime"),
		BackupPath:   backupPath,
	})
	if err == nil {
		t.Fatal("Restore() error = nil, want invalid marker error")
	}
}

func TestRestoreReplacesDatabaseAndPreservesCurrentCopy(t *testing.T) {
	extRoot := os.Getenv("GEODATA_SERVE_EXTENSION_DIR")
	if extRoot == "" {
		t.Skip("set GEODATA_SERVE_EXTENSION_DIR to a directory containing DuckDB spatial and httpfs extensions")
	}
	root := t.TempDir()
	runtimeDir := filepath.Dir(extRoot)
	extensionDir := extRoot
	database := filepath.Join(root, "current.duckdb")
	backupDir := filepath.Join(root, "backups")
	config := runtime.Config{
		DatabasePath: database,
		RuntimeDir:   runtimeDir,
		BackupDir:    backupDir,
		WorkingDir:   root,
		ExtensionDir: extensionDir,
	}
	rt, err := runtime.New(context.Background(), config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := collectRuntimeEvents(rt, runtime.Command{ID: "req_restore_one", Mode: runtime.ModeWrite, SQL: "CREATE TABLE saved (value INTEGER); INSERT INTO saved VALUES (1)"}); err != nil {
		t.Fatalf("first write error = %v", err)
	}
	if _, err := collectRuntimeEvents(rt, runtime.Command{ID: "req_restore_two", Mode: runtime.ModeWrite, SQL: "CREATE OR REPLACE TABLE saved AS SELECT 2::INTEGER AS value"}); err != nil {
		t.Fatalf("second write error = %v", err)
	}
	if err := rt.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("ReadDir(backups) error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("backup count = %d, want 2", len(entries))
	}
	selectedBackup := filepath.Join(backupDir, entries[1].Name())
	if err := Restore(context.Background(), Options{DatabasePath: database, RuntimeDir: runtimeDir, BackupPath: selectedBackup}); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}

	preserved, err := filepath.Glob(database + ".pre-restore-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(preserved) != 1 {
		t.Fatalf("preserved database count = %d, want 1", len(preserved))
	}
	rt, err = runtime.New(context.Background(), config)
	if err != nil {
		t.Fatalf("reopen New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := rt.Close(context.Background()); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	events, err := collectRuntimeEvents(rt, runtime.Command{ID: "req_restore_read", Mode: runtime.ModeRead, SQL: "SELECT value FROM saved"})
	if err != nil {
		t.Fatalf("restored read error = %v", err)
	}
	if len(events) != 5 || events[3].Values[0] != int32(1) {
		t.Fatalf("restored events = %#v, want value 1", events)
	}
}

func collectRuntimeEvents(rt runtime.Runtime, command runtime.Command) ([]runtime.Event, error) {
	var events []runtime.Event
	err := rt.Execute(context.Background(), command, func(event runtime.Event) error {
		events = append(events, event)
		return nil
	})
	return events, err
}
