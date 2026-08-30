package duckdbconn

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenConfiguresRelativeFilesPerConnectionWithoutChangingCWD(t *testing.T) {
	extensionDir := os.Getenv("GEODATA_SERVE_EXTENSION_DIR")
	if extensionDir == "" {
		t.Skip("set GEODATA_SERVE_EXTENSION_DIR to a directory containing DuckDB spatial and httpfs extensions")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "values.csv"), []byte("value\n11\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	handle, err := Open(context.Background(), Config{
		DatabasePath:   filepath.Join(root, "data.duckdb"),
		ExtensionDir:   extensionDir,
		TempDir:        filepath.Join(root, "tmp"),
		WorkingDir:     root,
		MaxOpenConns:   3,
		LoadExtensions: true,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := handle.DB.QueryRowContext(context.Background(), "SELECT current_setting('file_search_path')").Scan(new(string)); err != nil {
		t.Fatalf("read file_search_path: %v", err)
	}
	var value int32
	if err := handle.DB.QueryRowContext(context.Background(), "SELECT value::INTEGER FROM read_csv('values.csv', header=true)").Scan(&value); err != nil {
		t.Fatalf("read relative CSV: %v", err)
	}
	if value != 11 {
		t.Fatalf("relative CSV value = %d, want 11", value)
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	after, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("Open/Close changed process CWD from %q to %q", before, after)
	}
}
