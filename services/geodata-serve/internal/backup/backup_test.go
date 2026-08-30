package backup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestValidateRequiresCompleteMarkerAndSchema(t *testing.T) {
	root := t.TempDir()
	if _, err := Validate(root); err == nil {
		t.Fatal("Validate() accepted a directory without marker and schema")
	}
	if err := os.WriteFile(filepath.Join(root, VerifiedMarker), []byte(verifiedMarkerValue), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(root); err == nil {
		t.Fatal("Validate() accepted a directory without schema")
	}
	if err := os.WriteFile(filepath.Join(root, "schema.sql"), []byte("-- schema\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifact, err := Validate(root)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if artifact.Path() != root {
		t.Fatalf("artifact path = %q, want %q", artifact.Path(), root)
	}
}

func TestCleanupRetainsOnlyFiveValidArtifacts(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(Config{BackupDir: root, ExtensionDir: root, TempDir: root})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-time.Hour)
	for i := 0; i < 6; i++ {
		name := base.Add(time.Duration(i)*time.Second).UTC().Format("20060102T150405.000000000Z") + "-req_cleanup_" + string(rune('0'+i))
		path := filepath.Join(root, name)
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, VerifiedMarker), []byte(verifiedMarkerValue), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "schema.sql"), []byte("-- schema\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, base.Add(time.Duration(i)*time.Second), base.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	unknown := filepath.Join(root, "not-a-service-backup")
	if err := os.Mkdir(unknown, 0o700); err != nil {
		t.Fatal(err)
	}
	invalid := filepath.Join(root, base.Add(7*time.Second).UTC().Format("20060102T150405.000000000Z")+"-req_invalid")
	if err := os.Mkdir(invalid, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(invalid, VerifiedMarker), []byte("bad\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.cleanup(); err != nil {
		t.Fatalf("cleanup() error = %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 7 {
		t.Fatalf("entries after cleanup = %d, want five valid plus two untouched", len(entries))
	}
	if _, err := os.Stat(unknown); err != nil {
		t.Fatalf("unknown directory was removed: %v", err)
	}
	if _, err := os.Stat(invalid); err != nil {
		t.Fatalf("invalid artifact was removed: %v", err)
	}
	oldest := filepath.Join(root, base.UTC().Format("20060102T150405.000000000Z")+"-req_cleanup_0")
	if _, err := os.Stat(oldest); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("oldest valid artifact still exists: %v", err)
	}
}
