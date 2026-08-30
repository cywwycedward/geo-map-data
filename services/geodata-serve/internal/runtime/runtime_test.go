package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"services/geodata-serve/internal/backup"
	"services/geodata-serve/internal/bootstrap"
)

func newTestRuntime(t *testing.T) *RuntimeModule {
	t.Helper()
	extDir := os.Getenv("GEODATA_SERVE_EXTENSION_DIR")
	if extDir == "" {
		t.Skip("set GEODATA_SERVE_EXTENSION_DIR to a directory containing DuckDB spatial and httpfs extensions")
	}
	root := t.TempDir()
	rt, err := New(context.Background(), Config{
		DatabasePath: filepath.Join(root, "data.duckdb"),
		RuntimeDir:   root,
		BackupDir:    filepath.Join(root, "backups"),
		WorkingDir:   root,
		ExtensionDir: extDir,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	closeTestRuntime(t, rt)
	return rt
}

func closeTestRuntime(t *testing.T, rt *RuntimeModule) {
	t.Helper()
	t.Cleanup(func() {
		if err := rt.Close(context.Background()); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
}

func collectEvents(rt Runtime, command Command) ([]Event, error) {
	var events []Event
	err := rt.Execute(context.Background(), command, func(event Event) error {
		events = append(events, event)
		return nil
	})
	return events, err
}

func TestSafeErrorMessageKeepsDuckDBCauseWithoutSQLContext(t *testing.T) {
	err := errors.New("Catalog Error: Table with name missing_table does not exist!\nDid you mean \"pg_tables\"?\n\nLINE 1: SELECT * FROM missing_table\n                      ^")

	message := safeErrorMessage(err)
	if message != "Catalog Error: Table with name missing_table does not exist! Did you mean \"pg_tables\"?" {
		t.Fatalf("safeErrorMessage() = %q", message)
	}
	if strings.Contains(message, "SELECT *") || strings.Contains(message, "LINE 1") {
		t.Fatalf("safeErrorMessage() leaked SQL context: %q", message)
	}
}

func TestSafeErrorMessageRedactsCredentials(t *testing.T) {
	err := errors.New("HTTP Error: GET https://reader:password@example.test/data.parquet?token=secret-token&region=cn")

	message := safeErrorMessage(err)
	if !strings.Contains(message, "HTTP Error") {
		t.Fatalf("safeErrorMessage() = %q, want error cause", message)
	}
	for _, secret := range []string{"password", "secret-token"} {
		if strings.Contains(message, secret) {
			t.Fatalf("safeErrorMessage() leaked %q: %q", secret, message)
		}
	}
}

func TestRuntimePersistsDataAcrossReopen(t *testing.T) {
	extDir := os.Getenv("GEODATA_SERVE_EXTENSION_DIR")
	if extDir == "" {
		t.Skip("set GEODATA_SERVE_EXTENSION_DIR to a directory containing DuckDB spatial and httpfs extensions")
	}
	root := t.TempDir()
	config := Config{
		DatabasePath: filepath.Join(root, "data.duckdb"),
		RuntimeDir:   root,
		BackupDir:    filepath.Join(root, "backups"),
		ExtensionDir: extDir,
	}

	rt, err := New(context.Background(), config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	events := make([]Event, 0)
	if err := rt.Execute(context.Background(), Command{ID: "req_persist", Mode: ModeWrite, SQL: "CREATE TABLE persisted (value INTEGER); INSERT INTO persisted VALUES (7);"}, func(event Event) error {
		events = append(events, event)
		return nil
	}); err != nil {
		t.Fatalf("write Execute() error = %v", err)
	}
	if err := rt.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	rt, err = New(context.Background(), config)
	if err != nil {
		t.Fatalf("reopen New() error = %v", err)
	}
	closeTestRuntime(t, rt)
	if err := rt.Execute(context.Background(), Command{ID: "req_read", Mode: ModeRead, SQL: "SELECT value FROM persisted"}, func(event Event) error {
		events = append(events, event)
		return nil
	}); err != nil {
		t.Fatalf("read Execute() error = %v", err)
	}
	if len(events) == 0 {
		t.Fatal("Execute() emitted no events")
	}
}

func TestRuntimeReportsLoadedSpatialVersion(t *testing.T) {
	rt := newTestRuntime(t)
	if rt.SpatialVersion() == "" {
		t.Fatal("SpatialVersion() is empty")
	}
}

func TestRuntimeReopensAfterUnexpectedProcessExit(t *testing.T) {
	if os.Getenv("GEODATA_SERVE_UNEXPECTED_EXIT_HELPER") == "1" {
		runUnexpectedExitHelper()
		os.Exit(23)
	}
	extDir := os.Getenv("GEODATA_SERVE_EXTENSION_DIR")
	if extDir == "" {
		t.Skip("set GEODATA_SERVE_EXTENSION_DIR to a directory containing DuckDB spatial and httpfs extensions")
	}
	root := t.TempDir()
	database := filepath.Join(root, "data.duckdb")
	command := exec.Command(os.Args[0], "-test.run=^TestRuntimeReopensAfterUnexpectedProcessExit$")
	command.Env = append(os.Environ(),
		"GEODATA_SERVE_UNEXPECTED_EXIT_HELPER=1",
		"GEODATA_SERVE_UNEXPECTED_EXIT_DATABASE="+database,
		"GEODATA_SERVE_UNEXPECTED_EXIT_RUNTIME="+filepath.Join(root, "runtime"),
		"GEODATA_SERVE_UNEXPECTED_EXIT_BACKUPS="+filepath.Join(root, "backups"),
	)
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("crash helper exited successfully: %s", output)
	} else if _, ok := err.(*exec.ExitError); !ok {
		t.Fatalf("run crash helper: %v: %s", err, output)
	}
	rt, err := New(context.Background(), Config{
		DatabasePath: database,
		RuntimeDir:   filepath.Join(root, "runtime"),
		BackupDir:    filepath.Join(root, "backups"),
		WorkingDir:   root,
		ExtensionDir: extDir,
	})
	if err != nil {
		t.Fatalf("reopen after unexpected exit: %v", err)
	}
	closeTestRuntime(t, rt)
	events, err := collectEvents(rt, Command{ID: "req_reopen_after_exit", Mode: ModeRead, SQL: "SELECT value FROM survives_exit"})
	if err != nil {
		t.Fatalf("read after unexpected exit: %v", err)
	}
	if len(events) != 5 || events[3].Values[0] != int32(7) {
		t.Fatalf("reopened events = %#v, want persisted value 7", events)
	}
}

func runUnexpectedExitHelper() {
	rt, err := New(context.Background(), Config{
		DatabasePath: os.Getenv("GEODATA_SERVE_UNEXPECTED_EXIT_DATABASE"),
		RuntimeDir:   os.Getenv("GEODATA_SERVE_UNEXPECTED_EXIT_RUNTIME"),
		BackupDir:    os.Getenv("GEODATA_SERVE_UNEXPECTED_EXIT_BACKUPS"),
		WorkingDir:   filepath.Dir(os.Getenv("GEODATA_SERVE_UNEXPECTED_EXIT_DATABASE")),
		ExtensionDir: os.Getenv("GEODATA_SERVE_EXTENSION_DIR"),
	})
	if err != nil {
		os.Exit(1)
	}
	if _, err := collectEvents(rt, Command{ID: "req_before_unexpected_exit", Mode: ModeWrite, SQL: "CREATE TABLE survives_exit AS SELECT 7::INTEGER AS value"}); err != nil {
		os.Exit(2)
	}
}

func TestRuntimeEncodesValuesWithoutLossyJSONNumbers(t *testing.T) {
	rt := newTestRuntime(t)
	events, err := collectEvents(rt, Command{
		ID:   "req_values",
		Mode: ModeRead,
		SQL:  `SELECT CAST(9223372036854775807 AS BIGINT) AS big, CAST(1.25 AS DECIMAL(10,2)) AS decimal_value, [1, 2] AS list_value, {'a': 3} AS struct_value, CAST('abc' AS BLOB) AS blob_value, DATE '2026-08-28' AS date_value, INTERVAL 1 DAY AS interval_value`,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(events) != 5 || events[2].Type != "schema" || events[3].Type != "row" || events[4].Type != "summary" {
		t.Fatalf("events = %#v, want queued/running/schema/row/summary", events)
	}
	if got := events[3].Values[0]; got != "9223372036854775807" {
		t.Fatalf("BIGINT value = %#v, want decimal string", got)
	}
	if got := events[3].Values[1]; got != "1.25" {
		t.Fatalf("DECIMAL value = %#v, want decimal string", got)
	}
	if got := events[3].Values[4]; !reflect.DeepEqual(got, map[string]any{"encoding": "base64", "data": "YWJj"}) {
		t.Fatalf("BLOB value = %#v, want base64 object", got)
	}
	if got := events[3].Values[5]; got != "2026-08-28" {
		t.Fatalf("DATE value = %#v, want stable date text", got)
	}
	if got := events[3].Values[6]; got != "0 months 1 days 0 micros" {
		t.Fatalf("INTERVAL value = %#v, want stable interval text", got)
	}
	if _, err := json.Marshal(events[3].Values); err != nil {
		t.Fatalf("encoded values are not JSON: %v", err)
	}
}

func TestRuntimeEncodesNestedBigIntegersAsStrings(t *testing.T) {
	rt := newTestRuntime(t)
	events, err := collectEvents(rt, Command{
		ID:   "req_nested_bigints",
		Mode: ModeRead,
		SQL:  "SELECT [9223372036854775807::BIGINT] AS bigint_list, {'value': 9223372036854775807::BIGINT} AS bigint_struct",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("event count = %d, want 5", len(events))
	}
	want := []any{[]any{"9223372036854775807"}, map[string]any{"value": "9223372036854775807"}}
	if !reflect.DeepEqual(events[3].Values, want) {
		t.Fatalf("nested BIGINT values = %#v, want %#v", events[3].Values, want)
	}
}

func TestRuntimeRejectsJSONNumberThatDuckDBDriverCannotRepresentLosslessly(t *testing.T) {
	rt := newTestRuntime(t)
	events, err := collectEvents(rt, Command{
		ID:   "req_json_number",
		Mode: ModeRead,
		SQL:  `SELECT '{"large":9007199254740993,"nested":[-9223372036854775808]}'::JSON AS document`,
	})
	if !errors.Is(err, ErrResultEncoding) {
		t.Fatalf("Execute() error = %v, want result encoding failure", err)
	}
	if len(events) != 4 || events[3].Type != "error" {
		t.Fatalf("events = %#v, want queued/running/schema/error", events)
	}
	if events[3].Code != "result_encoding_failed" {
		t.Fatalf("terminal event = %#v, want result_encoding_failed", events[3])
	}
}

func TestRuntimeUsesWorkingDirectoryForRelativeFiles(t *testing.T) {
	root := t.TempDir()
	extDir := os.Getenv("GEODATA_SERVE_EXTENSION_DIR")
	if extDir == "" {
		t.Skip("set GEODATA_SERVE_EXTENSION_DIR to a directory containing DuckDB spatial and httpfs extensions")
	}
	file := filepath.Join(root, "values.csv")
	if err := os.WriteFile(file, []byte("value\n9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt, err := New(context.Background(), Config{
		DatabasePath: filepath.Join(root, "data.duckdb"),
		RuntimeDir:   filepath.Join(root, "runtime"),
		BackupDir:    filepath.Join(root, "backups"),
		WorkingDir:   root,
		ExtensionDir: extDir,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	closeTestRuntime(t, rt)
	events, err := collectEvents(rt, Command{ID: "req_relative", Mode: ModeRead, SQL: "SELECT value::INTEGER FROM read_csv('values.csv', header=true)"})
	if err != nil {
		t.Fatalf("relative file Execute() error = %v", err)
	}
	if got := events[3].Values[0]; got != int32(9) {
		t.Fatalf("relative file value = %#v, want int32(9)", got)
	}
}

func TestRuntimeDoesNotChangeProcessWorkingDirectory(t *testing.T) {
	extDir := os.Getenv("GEODATA_SERVE_EXTENSION_DIR")
	if extDir == "" {
		t.Skip("set GEODATA_SERVE_EXTENSION_DIR to a directory containing DuckDB spatial and httpfs extensions")
	}
	root := t.TempDir()
	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rt, err := New(context.Background(), Config{
		DatabasePath: filepath.Join(root, "data.duckdb"),
		RuntimeDir:   filepath.Join(root, "runtime"),
		BackupDir:    filepath.Join(root, "backups"),
		WorkingDir:   root,
		ExtensionDir: extDir,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	afterNew, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if afterNew != before {
		t.Fatalf("New() changed process working directory from %q to %q", before, afterNew)
	}
	if err := rt.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	afterClose, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if afterClose != before {
		t.Fatalf("Close() changed process working directory from %q to %q", before, afterClose)
	}
}

func TestRuntimeReadsGeoJSONFixtureThroughWorkingDirectory(t *testing.T) {
	extDir := os.Getenv("GEODATA_SERVE_EXTENSION_DIR")
	if extDir == "" {
		t.Skip("set GEODATA_SERVE_EXTENSION_DIR to a directory containing DuckDB spatial and httpfs extensions")
	}
	_, sourceFile, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatal("could not locate test file")
	}
	fixture, err := os.ReadFile(filepath.Join(filepath.Dir(sourceFile), "..", "..", "testdata", "points.geojson"))
	if err != nil {
		t.Fatalf("read GeoJSON fixture: %v", err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "points.geojson"), fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	restoreWorkingDir, err := bootstrap.EnterWorkingDirectory(root)
	if err != nil {
		t.Fatalf("EnterWorkingDirectory() error = %v", err)
	}
	rt, err := New(context.Background(), Config{
		DatabasePath: filepath.Join(root, "data.duckdb"),
		RuntimeDir:   filepath.Join(root, "runtime"),
		BackupDir:    filepath.Join(root, "backups"),
		WorkingDir:   root,
		ExtensionDir: extDir,
	})
	if err != nil {
		_ = restoreWorkingDir()
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := rt.Close(context.Background()); err != nil {
			t.Errorf("Close() error = %v", err)
		}
		if got, err := os.Getwd(); err != nil || got != root {
			t.Errorf("service CWD after Runtime close = %q, %v; want %q", got, err, root)
		}
		if err := restoreWorkingDir(); err != nil {
			t.Errorf("restore process working directory: %v", err)
		}
		if got, err := os.Getwd(); err != nil || got != before {
			t.Errorf("process CWD after lifecycle = %q, %v; want %q", got, err, before)
		}
	})
	events, err := collectEvents(rt, Command{ID: "req_geojson", Mode: ModeRead, SQL: "SELECT count(*)::INTEGER FROM ST_Read('points.geojson')"})
	if err != nil {
		t.Fatalf("GeoJSON Execute() error = %v", err)
	}
	if len(events) != 5 || events[3].Values[0] != int32(2) {
		t.Fatalf("GeoJSON events = %#v, want one row with count 2", events)
	}
}

func TestRuntimeLimitsReadsAndCancelsQueuedRead(t *testing.T) {
	rt := newTestRuntime(t)
	release := make(chan struct{})
	running := make(chan struct{}, 2)
	results := make(chan error, 2)
	for _, id := range []RequestID{"req_read_one", "req_read_two"} {
		go func(id RequestID) {
			_, err := collectEventsWithSink(rt, Command{ID: id, Mode: ModeRead, SQL: "SELECT 1"}, func(event Event) error {
				if event.Type == "status" && event.State == "running" {
					running <- struct{}{}
					<-release
				}
				return nil
			})
			results <- err
		}(id)
	}
	for range 2 {
		select {
		case <-running:
		case <-time.After(5 * time.Second):
			t.Fatal("two read requests did not enter running")
		}
	}

	queued := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	thirdDone := make(chan error, 1)
	go func() {
		thirdDone <- rt.Execute(ctx, Command{ID: "req_read_three", Mode: ModeRead, SQL: "SELECT 3"}, func(event Event) error {
			if event.Type == "status" && event.State == "queued" {
				close(queued)
			}
			return nil
		})
	}()
	select {
	case <-queued:
	case <-time.After(5 * time.Second):
		t.Fatal("third read request was not queued")
	}
	cancel()
	select {
	case err := <-thirdDone:
		if err == nil {
			t.Fatal("cancelled queued read returned nil error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled queued read did not finish")
	}
	status, ok := rt.Status("req_read_three")
	if !ok || status.State != "cancelled" {
		t.Fatalf("queued read status = %#v, found=%v, want cancelled", status, ok)
	}
	close(release)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("running read error = %v", err)
		}
	}
}

func collectEventsWithSink(rt Runtime, command Command, extra EventSink) ([]Event, error) {
	var events []Event
	err := rt.Execute(context.Background(), command, func(event Event) error {
		events = append(events, event)
		if extra != nil {
			return extra(event)
		}
		return nil
	})
	return events, err
}

func TestRuntimeCreatesVerifiedBackupBeforeWrite(t *testing.T) {
	extDir := os.Getenv("GEODATA_SERVE_EXTENSION_DIR")
	if extDir == "" {
		t.Skip("set GEODATA_SERVE_EXTENSION_DIR to a directory containing DuckDB spatial and httpfs extensions")
	}
	root := t.TempDir()
	rt, err := New(context.Background(), Config{
		DatabasePath: filepath.Join(root, "data.duckdb"),
		RuntimeDir:   filepath.Join(root, "runtime"),
		BackupDir:    filepath.Join(root, "backups"),
		WorkingDir:   root,
		ExtensionDir: extDir,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	closeTestRuntime(t, rt)
	events, err := collectEvents(rt, Command{ID: "req_backup", Mode: ModeWrite, SQL: "CREATE TABLE saved (value INTEGER); INSERT INTO saved VALUES (7)"})
	if err != nil {
		t.Fatalf("write Execute() error = %v", err)
	}
	var states []string
	for _, event := range events {
		if event.Type == "status" {
			states = append(states, event.State)
		}
	}
	if !reflect.DeepEqual(states, []string{"queued", "backing_up", "running"}) {
		t.Fatalf("write states = %#v, want queued/backing_up/running", states)
	}
	entries, err := os.ReadDir(filepath.Join(root, "backups"))
	if err != nil {
		t.Fatalf("ReadDir(backups) error = %v", err)
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("backup entries = %#v, want one directory", entries)
	}
	if _, err := os.Stat(filepath.Join(root, "backups", entries[0].Name(), backup.VerifiedMarker)); err != nil {
		t.Fatalf("verified backup marker error = %v", err)
	}
}

func TestRuntimeRetainsOnlyFiveVerifiedBackups(t *testing.T) {
	extDir := os.Getenv("GEODATA_SERVE_EXTENSION_DIR")
	if extDir == "" {
		t.Skip("set GEODATA_SERVE_EXTENSION_DIR to a directory containing DuckDB spatial and httpfs extensions")
	}
	root := t.TempDir()
	rt, err := New(context.Background(), Config{
		DatabasePath: filepath.Join(root, "data.duckdb"),
		RuntimeDir:   filepath.Join(root, "runtime"),
		BackupDir:    filepath.Join(root, "backups"),
		WorkingDir:   root,
		ExtensionDir: extDir,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	closeTestRuntime(t, rt)
	for i := 0; i < 6; i++ {
		command := Command{ID: RequestID(fmt.Sprintf("req_retention_%d", i)), Mode: ModeWrite, SQL: fmt.Sprintf("CREATE OR REPLACE TABLE retained AS SELECT %d::INTEGER AS value", i)}
		if _, err := collectEvents(rt, command); err != nil {
			t.Fatalf("write %d error = %v", i, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "backups"))
	if err != nil {
		t.Fatalf("ReadDir(backups) error = %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("backup count = %d, want 5", len(entries))
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			t.Fatalf("backup entry %q is not a directory", entry.Name())
		}
		if _, err := os.Stat(filepath.Join(root, "backups", entry.Name(), backup.VerifiedMarker)); err != nil {
			t.Fatalf("backup %q marker error = %v", entry.Name(), err)
		}
	}
}

func TestRuntimePreservesWriteAcceptanceOrderWhenFirstClientBlocks(t *testing.T) {
	rt := newTestRuntime(t)
	firstQueued := make(chan struct{})
	secondBackedUp := make(chan struct{})
	releaseFirst := make(chan struct{})
	release := func() {
		select {
		case <-releaseFirst:
		default:
			close(releaseFirst)
		}
	}
	defer release()
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)

	go func() {
		firstDone <- rt.Execute(context.Background(), Command{
			ID:   "req_write_first",
			Mode: ModeWrite,
			SQL:  "CREATE TABLE write_order (value INTEGER); INSERT INTO write_order VALUES (1)",
		}, func(event Event) error {
			if event.Type == "status" && event.State == "queued" {
				close(firstQueued)
				<-releaseFirst
			}
			return nil
		})
	}()
	select {
	case <-firstQueued:
	case <-time.After(5 * time.Second):
		t.Fatal("first write did not publish queued")
	}

	go func() {
		secondDone <- rt.Execute(context.Background(), Command{
			ID:   "req_write_second",
			Mode: ModeWrite,
			SQL:  "CREATE TABLE IF NOT EXISTS write_order (value INTEGER); INSERT INTO write_order VALUES (2)",
		}, func(event Event) error {
			if event.Type == "status" && event.State == "backing_up" {
				close(secondBackedUp)
			}
			return nil
		})
	}()

	select {
	case <-secondBackedUp:
		t.Fatal("later write entered backing_up before earlier accepted write was released")
	case <-time.After(500 * time.Millisecond):
	}
	release()
	if err := <-firstDone; err != nil {
		t.Fatalf("first write error = %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second write error = %v", err)
	}
	events, err := collectEvents(rt, Command{ID: "req_write_order", Mode: ModeRead, SQL: "SELECT value FROM write_order ORDER BY rowid"})
	if err != nil {
		t.Fatalf("read order error = %v", err)
	}
	if !reflect.DeepEqual(events[3].Values, []any{int32(1)}) || !reflect.DeepEqual(events[4].Values, []any{int32(2)}) {
		t.Fatalf("write order rows = %#v, %#v, want 1 then 2", events[3].Values, events[4].Values)
	}
}

func TestRuntimeCancelsQueuedWriteBeforeItReachesWorker(t *testing.T) {
	rt := newTestRuntime(t)
	firstBackedUp := make(chan struct{})
	secondQueued := make(chan struct{})
	releaseFirst := make(chan struct{})
	release := func() {
		select {
		case <-releaseFirst:
		default:
			close(releaseFirst)
		}
	}
	defer release()
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)

	go func() {
		firstDone <- rt.Execute(context.Background(), Command{
			ID:   "req_blocking_write",
			Mode: ModeWrite,
			SQL:  "CREATE TABLE blocking_write (value INTEGER); INSERT INTO blocking_write VALUES (1)",
		}, func(event Event) error {
			if event.Type == "status" && event.State == "backing_up" {
				close(firstBackedUp)
				<-releaseFirst
			}
			return nil
		})
	}()
	select {
	case <-firstBackedUp:
	case <-time.After(5 * time.Second):
		t.Fatal("first write did not enter backing_up")
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		secondDone <- rt.Execute(ctx, Command{
			ID:   "req_cancelled_write",
			Mode: ModeWrite,
			SQL:  "CREATE TABLE must_not_exist (value INTEGER)",
		}, func(event Event) error {
			if event.Type == "status" && event.State == "queued" {
				close(secondQueued)
			}
			return nil
		})
	}()
	select {
	case <-secondQueued:
	case <-time.After(5 * time.Second):
		t.Fatal("second write did not publish queued")
	}
	cancel()
	select {
	case err := <-secondDone:
		if err == nil {
			t.Fatal("cancelled queued write returned nil")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled queued write did not return promptly")
	}
	status, ok := rt.Status("req_cancelled_write")
	if !ok || status.State != "cancelled" {
		t.Fatalf("cancelled write status = %#v, found=%v", status, ok)
	}
	release()
	if err := <-firstDone; err != nil {
		t.Fatalf("first write error = %v", err)
	}
}

func TestRuntimeDoesNotExecuteWriteWithAlreadyCancelledContext(t *testing.T) {
	rt := newTestRuntime(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	events := make([]Event, 0)
	err := rt.Execute(ctx, Command{
		ID:   "req_pre_cancelled_write",
		Mode: ModeWrite,
		SQL:  "CREATE TABLE pre_cancelled_write (value INTEGER)",
	}, func(event Event) error {
		events = append(events, event)
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute() error = %v, want context cancellation", err)
	}
	if len(events) != 2 || events[0].State != "queued" || events[1].Type != "error" || events[1].State != "cancelled" {
		t.Fatalf("events = %#v, want queued then cancelled terminal event", events)
	}
	verify, err := collectEvents(rt, Command{
		ID:   "req_verify_pre_cancelled_write",
		Mode: ModeRead,
		SQL:  "SELECT count(*)::INTEGER FROM information_schema.tables WHERE table_name = 'pre_cancelled_write'",
	})
	if err != nil {
		t.Fatalf("verify pre-cancelled write: %v", err)
	}
	if got := verify[3].Values[0]; got != int32(0) {
		t.Fatalf("pre-cancelled write table count = %#v, want 0", got)
	}
}

func TestRuntimeDoesNotExecuteWriteCancelledDuringQueuedEventOnShutdown(t *testing.T) {
	extDir := os.Getenv("GEODATA_SERVE_EXTENSION_DIR")
	if extDir == "" {
		t.Skip("set GEODATA_SERVE_EXTENSION_DIR to a directory containing DuckDB spatial and httpfs extensions")
	}
	root := t.TempDir()
	config := Config{
		DatabasePath: filepath.Join(root, "data.duckdb"),
		RuntimeDir:   filepath.Join(root, "runtime"),
		BackupDir:    filepath.Join(root, "backups"),
		WorkingDir:   root,
		ExtensionDir: extDir,
	}
	rt, err := New(context.Background(), config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	queued := make(chan struct{})
	releaseQueuedEvent := make(chan struct{})
	requestDone := make(chan error, 1)
	go func() {
		requestDone <- rt.Execute(context.Background(), Command{
			ID:   "req_shutdown_queued_write",
			Mode: ModeWrite,
			SQL:  "CREATE TABLE must_not_survive_shutdown (value INTEGER)",
		}, func(event Event) error {
			if event.Type == "status" && event.State == "queued" {
				close(queued)
				<-releaseQueuedEvent
			}
			return nil
		})
	}()
	select {
	case <-queued:
	case <-time.After(5 * time.Second):
		t.Fatal("write did not emit queued status")
	}
	closeDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		closeDone <- rt.Close(ctx)
	}()
	close(releaseQueuedEvent)
	select {
	case err := <-requestDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled write error = %v, want context cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("write did not finish during shutdown")
	}
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close() did not finish")
	}
	rt, err = New(context.Background(), config)
	if err != nil {
		t.Fatalf("reopen after shutdown: %v", err)
	}
	closeTestRuntime(t, rt)
	events, err := collectEvents(rt, Command{
		ID:   "req_verify_shutdown_queued_write",
		Mode: ModeRead,
		SQL:  "SELECT count(*)::INTEGER FROM information_schema.tables WHERE table_name = 'must_not_survive_shutdown'",
	})
	if err != nil {
		t.Fatalf("verify cancelled shutdown write: %v", err)
	}
	if got := events[3].Values[0]; got != int32(0) {
		t.Fatalf("shutdown-cancelled write table count = %#v, want 0", got)
	}
}

func TestRuntimeContinuesAfterWriteQueuedEventCannotBeDelivered(t *testing.T) {
	rt := newTestRuntime(t)
	sinkErr := errors.New("client disconnected before queued event")
	err := rt.Execute(context.Background(), Command{
		ID:   "req_unwritten_queued_event",
		Mode: ModeWrite,
		SQL:  "CREATE TABLE must_not_be_created (value INTEGER)",
	}, func(event Event) error {
		if event.Type == "status" && event.State == "queued" {
			return sinkErr
		}
		return nil
	})
	if !errors.Is(err, sinkErr) {
		t.Fatalf("Execute() error = %v, want queued sink error", err)
	}
	if _, err := collectEvents(rt, Command{
		ID:   "req_write_after_sink_error",
		Mode: ModeWrite,
		SQL:  "CREATE TABLE write_after_sink_error (value INTEGER)",
	}); err != nil {
		t.Fatalf("write after queued sink error = %v", err)
	}
	events, err := collectEvents(rt, Command{
		ID:   "req_verify_queued_sink_error",
		Mode: ModeRead,
		SQL:  "SELECT count(*)::INTEGER FROM information_schema.tables WHERE table_name = 'must_not_be_created'",
	})
	if err != nil {
		t.Fatalf("verify interrupted write error = %v", err)
	}
	if got := events[3].Values[0]; got != int32(0) {
		t.Fatalf("write with failed queued event created table count = %#v, want 0", got)
	}
}

func TestRuntimeCloseCancelsRunningRequest(t *testing.T) {
	rt := newTestRuntime(t)
	running := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- rt.Execute(context.Background(), Command{
			ID:   "req_close_cancel",
			Mode: ModeRead,
			SQL:  "SELECT sum(random()) FROM range(1000000000)",
		}, func(event Event) error {
			if event.Type == "status" && event.State == "running" {
				close(running)
			}
			return nil
		})
	}()
	select {
	case <-running:
	case <-time.After(5 * time.Second):
		t.Fatal("query did not enter running")
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rt.Close(closeCtx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled running request returned nil")
		}
	case <-time.After(time.Second):
		t.Fatal("running request did not finish after Close")
	}
	status, ok := rt.Status("req_close_cancel")
	if !ok || status.State != "cancelled" {
		t.Fatalf("closed request status = %#v, found=%v", status, ok)
	}
}

func TestRuntimeBeginShutdownCancelsRequestsBeforeClosingDatabase(t *testing.T) {
	rt := newTestRuntime(t)
	running := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- rt.Execute(context.Background(), Command{
			ID:   "req_begin_shutdown",
			Mode: ModeRead,
			SQL:  "SELECT sum(random()) FROM range(1000000000)",
		}, func(event Event) error {
			if event.Type == "status" && event.State == "running" {
				close(running)
			}
			return nil
		})
	}()
	select {
	case <-running:
	case <-time.After(5 * time.Second):
		t.Fatal("query did not enter running")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rt.BeginShutdown(ctx); err != nil {
		t.Fatalf("BeginShutdown() error = %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled request error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("request did not finish during BeginShutdown")
	}
	if err := rt.Execute(context.Background(), Command{ID: "req_after_begin_shutdown", Mode: ModeRead, SQL: "SELECT 1"}, nil); !errors.Is(err, ErrShuttingDown) {
		t.Fatalf("Execute() after BeginShutdown error = %v, want shutting down", err)
	}
	if err := rt.Close(ctx); err != nil {
		t.Fatalf("Close() after BeginShutdown error = %v", err)
	}
}

func TestRuntimeCancelsRunningWriteAndRollsBackTransaction(t *testing.T) {
	rt := newTestRuntime(t)
	running := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- rt.Execute(ctx, Command{
			ID:   "req_cancel_running_write",
			Mode: ModeWrite,
			SQL:  "BEGIN; CREATE TABLE cancelled_write (value INTEGER); INSERT INTO cancelled_write VALUES (1); SELECT sum(random()) FROM range(1000000000); COMMIT;",
		}, func(event Event) error {
			if event.Type == "status" && event.State == "running" {
				close(running)
			}
			return nil
		})
	}()
	select {
	case <-running:
	case <-time.After(5 * time.Second):
		t.Fatal("write did not enter running")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled write error = %v, want context cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled write did not finish")
	}
	events, err := collectEvents(rt, Command{
		ID:   "req_cancelled_write_verify",
		Mode: ModeRead,
		SQL:  "SELECT count(*)::INTEGER FROM information_schema.tables WHERE table_name = 'cancelled_write'",
	})
	if err != nil {
		t.Fatalf("verify transaction rollback error = %v", err)
	}
	if got := events[3].Values[0]; got != int32(0) {
		t.Fatalf("cancelled transaction left table count = %#v, want 0", got)
	}
}

func TestRuntimeRunsWriteAlongsideTwoReads(t *testing.T) {
	rt := newTestRuntime(t)
	releaseReads := make(chan struct{})
	readSchemas := make(chan struct{}, 2)
	readDone := make(chan error, 2)
	for _, id := range []RequestID{"req_parallel_read_one", "req_parallel_read_two"} {
		go func(id RequestID) {
			readDone <- rt.Execute(context.Background(), Command{ID: id, Mode: ModeRead, SQL: "SELECT * FROM range(1)"}, func(event Event) error {
				if event.Type == "schema" {
					readSchemas <- struct{}{}
					<-releaseReads
				}
				return nil
			})
		}(id)
	}
	for range 2 {
		select {
		case <-readSchemas:
		case <-time.After(5 * time.Second):
			t.Fatal("reads did not reach schema")
		}
	}

	writeFinished := make(chan struct{})
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- rt.Execute(context.Background(), Command{ID: "req_parallel_write", Mode: ModeWrite, SQL: "CREATE TABLE concurrent_write (value INTEGER)"}, func(event Event) error {
			if event.Type == "summary" {
				close(writeFinished)
			}
			return nil
		})
	}()
	select {
	case <-writeFinished:
	case <-time.After(5 * time.Second):
		t.Fatal("write did not finish while reads were active")
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("parallel write error = %v", err)
	}
	close(releaseReads)
	for range 2 {
		if err := <-readDone; err != nil {
			t.Fatalf("parallel read error = %v", err)
		}
	}
}

func TestRuntimeLoadsSpatialAndHTTPFSOnEachConcurrentConnection(t *testing.T) {
	rt := newTestRuntime(t)
	release := make(chan struct{})
	schemas := make(chan struct{}, 2)
	done := make(chan struct {
		events []Event
		err    error
	}, 2)
	for _, id := range []RequestID{"req_extensions_one", "req_extensions_two"} {
		go func(id RequestID) {
			var events []Event
			err := rt.Execute(context.Background(), Command{
				ID:   id,
				Mode: ModeRead,
				SQL:  "SELECT extension_name, loaded FROM duckdb_extensions() WHERE extension_name IN ('httpfs', 'spatial') ORDER BY extension_name",
			}, func(event Event) error {
				events = append(events, event)
				if event.Type == "schema" {
					schemas <- struct{}{}
					<-release
				}
				return nil
			})
			done <- struct {
				events []Event
				err    error
			}{events: events, err: err}
		}(id)
	}
	for range 2 {
		select {
		case <-schemas:
		case <-time.After(5 * time.Second):
			t.Fatal("connections did not reach schema")
		}
	}
	close(release)
	for range 2 {
		result := <-done
		if result.err != nil {
			t.Fatalf("extension query error = %v", result.err)
		}
		if len(result.events) != 6 || !reflect.DeepEqual(result.events[3].Values, []any{"httpfs", true}) || !reflect.DeepEqual(result.events[4].Values, []any{"spatial", true}) {
			t.Fatalf("extension events = %#v", result.events)
		}
	}
}

func TestRuntimeDoesNotExecuteWriteWhenBackupFails(t *testing.T) {
	extDir := os.Getenv("GEODATA_SERVE_EXTENSION_DIR")
	if extDir == "" {
		t.Skip("set GEODATA_SERVE_EXTENSION_DIR to a directory containing DuckDB spatial and httpfs extensions")
	}
	root := t.TempDir()
	backupDir := filepath.Join(root, "backups")
	rt, err := New(context.Background(), Config{
		DatabasePath: filepath.Join(root, "data.duckdb"),
		RuntimeDir:   filepath.Join(root, "runtime"),
		BackupDir:    backupDir,
		WorkingDir:   root,
		ExtensionDir: extDir,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	closeTestRuntime(t, rt)
	if err := os.Remove(backupDir); err != nil {
		t.Fatalf("remove backup directory: %v", err)
	}
	if err := os.WriteFile(backupDir, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("replace backup directory: %v", err)
	}
	events, err := collectEvents(rt, Command{ID: "req_backup_failure", Mode: ModeWrite, SQL: "CREATE TABLE must_not_run (value INTEGER)"})
	if !errors.Is(err, ErrBackupFailed) {
		t.Fatalf("write error = %v, want ErrBackupFailed", err)
	}
	if len(events) != 3 || events[2].Type != "error" || events[2].Code != "backup_failed" {
		t.Fatalf("backup failure events = %#v", events)
	}
	readEvents, err := collectEvents(rt, Command{ID: "req_backup_failure_check", Mode: ModeRead, SQL: "SELECT count(*)::INTEGER FROM information_schema.tables WHERE table_name = 'must_not_run'"})
	if err != nil {
		t.Fatalf("read after backup failure error = %v", err)
	}
	if readEvents[3].Values[0] != int32(0) {
		t.Fatalf("write ran despite failed backup: %#v", readEvents[3].Values)
	}
}
