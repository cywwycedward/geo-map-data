package duckdbutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathWithinRejectsRootAndParent(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	parent := filepath.Dir(root)
	if !PathWithin(root, child) {
		t.Fatal("child path was rejected")
	}
	if PathWithin(root, root) {
		t.Fatal("root path was accepted")
	}
	if PathWithin(root, parent) {
		t.Fatal("parent path was accepted")
	}
}

func TestEmptyExportSchemaRecognizesWhitespaceOnlySchema(t *testing.T) {
	backup := t.TempDir()
	if err := os.WriteFile(filepath.Join(backup, "schema.sql"), []byte("\n\t"), 0o600); err != nil {
		t.Fatal(err)
	}
	empty, err := EmptyExportSchema(backup)
	if err != nil {
		t.Fatal(err)
	}
	if !empty {
		t.Fatal("whitespace schema was not empty")
	}
}
