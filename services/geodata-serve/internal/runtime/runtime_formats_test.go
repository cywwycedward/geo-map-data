package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"services/geodata-serve/internal/bootstrap"
)

func TestRuntimeReadsGeoParquetAndShapefileFixtures(t *testing.T) {
	extDir := os.Getenv("GEODATA_SERVE_EXTENSION_DIR")
	if extDir == "" {
		t.Skip("set GEODATA_SERVE_EXTENSION_DIR to a directory containing DuckDB spatial and httpfs extensions")
	}
	root := t.TempDir()
	fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "points.geojson"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "points.geojson"), fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	restoreWorkingDir, err := bootstrap.EnterWorkingDirectory(root)
	if err != nil {
		t.Fatalf("EnterWorkingDirectory() error = %v", err)
	}
	t.Cleanup(func() {
		if err := restoreWorkingDir(); err != nil {
			t.Errorf("restore process working directory: %v", err)
		}
	})
	rt, err := New(context.Background(), Config{
		DatabasePath: filepath.Join(root, "data.duckdb"),
		RuntimeDir:   filepath.Join(root, "runtime"),
		BackupDir:    filepath.Join(root, "backups"),
		WorkingDir:   root,
		ExtensionDir: extDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	closeTestRuntime(t, rt)
	if _, err := collectEvents(rt, Command{ID: "req_formats_write", Mode: ModeWrite, SQL: "CREATE OR REPLACE TABLE points AS SELECT * FROM ST_Read('points.geojson'); COPY points TO 'points.parquet' (FORMAT PARQUET); COPY points TO 'points.shp' WITH (FORMAT GDAL, DRIVER 'ESRI Shapefile', SRS 'EPSG:4326')"}); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"points.parquet", "points.shp", "points.shx", "points.dbf"} {
		if _, err := os.Stat(filepath.Join(root, file)); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
	}
	queries := []struct {
		id    RequestID
		query string
	}{
		{id: "req_parquet_read", query: "SELECT count(*)::INTEGER FROM read_parquet('points.parquet')"},
		{id: "req_shapefile_read", query: "SELECT count(*)::INTEGER FROM ST_Read('points.shp')"},
	}
	for i, test := range queries {
		events, err := collectEvents(rt, Command{ID: test.id, Mode: ModeRead, SQL: test.query})
		if err != nil {
			t.Fatalf("read format %d: %v", i, err)
		}
		if len(events) != 5 || events[3].Values[0] != int32(2) {
			t.Fatalf("read format %d events = %#v", i, events)
		}
	}
}

func TestRuntimeRunsWuhanUniversityRoadsImportAndAnalysisScenario(t *testing.T) {
	extDir := os.Getenv("GEODATA_SERVE_EXTENSION_DIR")
	if extDir == "" {
		t.Skip("set GEODATA_SERVE_EXTENSION_DIR to a directory containing DuckDB spatial and httpfs extensions")
	}
	root := t.TempDir()
	fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "wuhan_university_roads.geojson"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "wuhan_university_roads.geojson"), fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	restoreWorkingDir, err := bootstrap.EnterWorkingDirectory(root)
	if err != nil {
		t.Fatalf("EnterWorkingDirectory() error = %v", err)
	}
	t.Cleanup(func() {
		if err := restoreWorkingDir(); err != nil {
			t.Errorf("restore process working directory: %v", err)
		}
	})
	rt, err := New(context.Background(), Config{
		DatabasePath: filepath.Join(root, "data.duckdb"),
		RuntimeDir:   filepath.Join(root, "runtime"),
		BackupDir:    filepath.Join(root, "backups"),
		WorkingDir:   root,
		ExtensionDir: extDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	closeTestRuntime(t, rt)
	if _, err := collectEvents(rt, Command{
		ID:   "req_wuhan_roads_import",
		Mode: ModeWrite,
		SQL:  "CREATE TABLE wuhan_university_roads AS SELECT name, geom FROM ST_Read('wuhan_university_roads.geojson')",
	}); err != nil {
		t.Fatalf("import roads: %v", err)
	}
	events, err := collectEvents(rt, Command{
		ID:   "req_wuhan_roads_analysis",
		Mode: ModeRead,
		SQL:  "SELECT count(*)::INTEGER AS road_count, sum(ST_Length(geom))::DOUBLE AS total_length FROM wuhan_university_roads",
	})
	if err != nil {
		t.Fatalf("analyse roads: %v", err)
	}
	if len(events) != 5 || events[3].Values[0] != int32(2) {
		t.Fatalf("road analysis events = %#v, want two roads", events)
	}
	if totalLength, ok := events[3].Values[1].(float64); !ok || totalLength <= 0 {
		t.Fatalf("road total length = %#v, want positive floating-point value", events[3].Values[1])
	}
}
