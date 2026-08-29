package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"services/geodata-serve/internal/contract"
	"services/geodata-serve/internal/runtime"
)

type fakeRuntime struct {
	status contract.RequestStatus
	closed bool
}

type errorRuntime struct{}

type blockingRuntime struct {
	started   chan struct{}
	cancelled chan struct{}
}

type shutdownRuntime struct {
	mu        sync.Mutex
	cancel    context.CancelFunc
	started   chan struct{}
	cancelled chan struct{}
}

func (errorRuntime) Execute(context.Context, contract.Command, contract.EventSink) error {
	return errors.New("runtime failed")
}

func (errorRuntime) Status(contract.RequestID) (contract.RequestStatus, bool) {
	return contract.RequestStatus{}, false
}

func (errorRuntime) Close(context.Context) error { return nil }

func (b *blockingRuntime) Execute(ctx context.Context, command contract.Command, sink contract.EventSink) error {
	if err := sink(contract.Event{Type: "status", RequestID: command.ID, State: "queued", At: time.Now().UTC()}); err != nil {
		return err
	}
	if err := sink(contract.Event{Type: "status", RequestID: command.ID, State: "running", At: time.Now().UTC()}); err != nil {
		return err
	}
	close(b.started)
	<-ctx.Done()
	close(b.cancelled)
	return ctx.Err()
}

func (b *blockingRuntime) Status(contract.RequestID) (contract.RequestStatus, bool) {
	return contract.RequestStatus{}, false
}

func (b *blockingRuntime) Close(context.Context) error { return nil }

func (s *shutdownRuntime) Execute(ctx context.Context, command contract.Command, sink contract.EventSink) error {
	runCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
	defer cancel()
	if err := sink(contract.Event{Type: "status", RequestID: command.ID, State: "queued", At: time.Now().UTC()}); err != nil {
		return err
	}
	if err := sink(contract.Event{Type: "status", RequestID: command.ID, State: "running", At: time.Now().UTC()}); err != nil {
		return err
	}
	close(s.started)
	<-runCtx.Done()
	close(s.cancelled)
	return runCtx.Err()
}

func (s *shutdownRuntime) Status(contract.RequestID) (contract.RequestStatus, bool) {
	return contract.RequestStatus{}, false
}

func (s *shutdownRuntime) Close(context.Context) error {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (f *fakeRuntime) Execute(_ context.Context, command contract.Command, sink contract.EventSink) error {
	if err := sink(contract.Event{Type: "status", RequestID: command.ID, State: "queued", At: time.Now().UTC()}); err != nil {
		return err
	}
	if err := sink(contract.Event{Type: "status", RequestID: command.ID, State: "running", At: time.Now().UTC()}); err != nil {
		return err
	}
	if err := sink(contract.Event{Type: "schema", RequestID: command.ID, Columns: []contract.Column{{Name: "value", DuckDBType: "INTEGER"}, {Name: "value", DuckDBType: "VARCHAR"}}}); err != nil {
		return err
	}
	if err := sink(contract.Event{Type: "row", RequestID: command.ID, Values: []any{int64(7), "seven"}}); err != nil {
		return err
	}
	return sink(contract.Event{Type: "summary", RequestID: command.ID, State: "finished", RowCount: pointer(int64(1))})
}

func (f *fakeRuntime) Status(id contract.RequestID) (contract.RequestStatus, bool) {
	if id != f.status.RequestID {
		return contract.RequestStatus{}, false
	}
	return f.status, true
}

func (f *fakeRuntime) Close(context.Context) error {
	f.closed = true
	return nil
}

func pointer[T any](value T) *T { return &value }

func closeRuntime(t *testing.T, rt *runtime.RuntimeModule) {
	t.Helper()
	t.Cleanup(func() {
		if err := rt.Close(context.Background()); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
}

func closeResponseBody(t *testing.T, response *http.Response) {
	t.Helper()
	t.Cleanup(func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close response body: %v", err)
		}
	})
}

func TestExecuteRequiresBearerTokenAndStrictJSON(t *testing.T) {
	server := New(Config{Runtime: &fakeRuntime{}, Token: "test-token", ServiceVersion: "0.1.0", DuckDBVersion: "1.4.5"})
	handler := server.Handler()

	req := httptest.NewRequest(http.MethodPost, "/execute", strings.NewReader(`{"mode":"read","sql":"SELECT 1"}`))
	req.Header.Set("Content-Type", "application/json")
	withoutToken := httptest.NewRecorder()
	handler.ServeHTTP(withoutToken, req)
	if withoutToken.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d, want 401", withoutToken.Code)
	}
	if strings.Contains(withoutToken.Body.String(), "test-token") {
		t.Fatal("token appeared in response")
	}

	req = httptest.NewRequest(http.MethodPost, "/execute", strings.NewReader(`{"mode":"read","sql":"SELECT 1","unexpected":true}`))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	unknownField := httptest.NewRecorder()
	handler.ServeHTTP(unknownField, req)
	if unknownField.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d, want 400", unknownField.Code)
	}
	var errorBody map[string]any
	if err := json.Unmarshal(unknownField.Body.Bytes(), &errorBody); err != nil {
		t.Fatalf("unknown field response is not JSON: %v", err)
	}
}

func TestExecuteAuthenticatesBeforeReportingWrongMethod(t *testing.T) {
	server := New(Config{Runtime: &fakeRuntime{}, Token: "test-token"})
	req := httptest.NewRequest(http.MethodGet, "/execute", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, req)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated wrong method status = %d, want 401", response.Code)
	}

	req.Header.Set("Authorization", "Bearer test-token")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, req)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("authenticated wrong method status = %d, want 405", response.Code)
	}
}

func TestExecuteStreamsSchemaRowsAndSummaryInOrder(t *testing.T) {
	server := New(Config{Runtime: &fakeRuntime{}, Token: "test-token", ServiceVersion: "0.1.0", DuckDBVersion: "1.4.5"})
	req := httptest.NewRequest(http.MethodPost, "/execute", strings.NewReader(`{"mode":"read","sql":"SELECT 1"}`))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, req)

	if response.Code != http.StatusOK {
		t.Fatalf("execute status = %d, want 200", response.Code)
	}
	if response.Header().Get("Content-Type") != "application/x-ndjson; charset=utf-8" {
		t.Fatalf("content type = %q", response.Header().Get("Content-Type"))
	}
	if response.Header().Get("X-Request-ID") == "" {
		t.Fatal("X-Request-ID is missing")
	}
	requestID := response.Header().Get("X-Request-ID")
	lines := strings.Split(strings.TrimSpace(response.Body.String()), "\n")
	if len(lines) != 5 {
		t.Fatalf("event count = %d, want 5: %s", len(lines), response.Body.String())
	}
	var events []map[string]any
	for _, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid NDJSON line %q: %v", line, err)
		}
		events = append(events, event)
	}
	wantTypes := []string{"status", "status", "schema", "row", "summary"}
	for i, want := range wantTypes {
		if events[i]["type"] != want {
			t.Errorf("event %d type = %v, want %s", i, events[i]["type"], want)
		}
		if events[i]["request_id"] != requestID {
			t.Errorf("event %d request_id = %v, want %s", i, events[i]["request_id"], requestID)
		}
	}
	columns := events[2]["columns"].([]any)
	if len(columns) != 2 {
		t.Fatalf("schema columns = %d, want duplicate columns preserved", len(columns))
	}
}

func TestExecuteReturnsJSONWhenRuntimeFailsBeforeStreaming(t *testing.T) {
	server := New(Config{Runtime: errorRuntime{}, Token: "test-token"})
	req := httptest.NewRequest(http.MethodPost, "/execute", strings.NewReader(`{"mode":"read","sql":"SELECT 1"}`))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, req)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("execute status = %d, want 500", response.Code)
	}
	var responseBody map[string]map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &responseBody); err != nil {
		t.Fatalf("invalid JSON error response: %v", err)
	}
	if responseBody["error"]["code"] != "internal_error" {
		t.Fatalf("runtime failure response = %#v, want internal_error", responseBody)
	}
}

func TestExecuteRejectsTimeoutThatOverflowsDuration(t *testing.T) {
	server := New(Config{Runtime: &fakeRuntime{}, Token: "test-token"})
	req := httptest.NewRequest(http.MethodPost, "/execute", strings.NewReader(`{"mode":"read","sql":"SELECT 1","timeout_seconds":9223372037}`))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, req)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("timeout overflow status = %d, want 400", response.Code)
	}
}

func TestShutdownRejectsNewExecutionRequests(t *testing.T) {
	shutdownCalled := make(chan struct{})
	server := New(Config{
		Runtime: &fakeRuntime{},
		Token:   "test-token",
		Shutdown: func() {
			close(shutdownCalled)
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/shutdown", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, req)
	if response.Code != http.StatusAccepted {
		t.Fatalf("shutdown status = %d, want 202", response.Code)
	}
	select {
	case <-shutdownCalled:
	case <-time.After(time.Second):
		t.Fatal("shutdown callback was not called")
	}

	req = httptest.NewRequest(http.MethodPost, "/execute", strings.NewReader(`{"mode":"read","sql":"SELECT 1"}`))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, req)
	if response.Code != http.StatusConflict {
		t.Fatalf("execute after shutdown status = %d, want 409", response.Code)
	}
}

func TestRequestStatusUsesRuntimeStateWithoutDatabaseAccess(t *testing.T) {
	fake := &fakeRuntime{status: contract.RequestStatus{
		RequestID:  "req_known",
		Mode:       contract.ModeRead,
		State:      "running",
		AcceptedAt: time.Date(2026, time.August, 28, 0, 0, 0, 0, time.UTC),
	}}
	server := New(Config{Runtime: fake, Token: "test-token"})
	req := httptest.NewRequest(http.MethodGet, "/requests/req_known", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("request status = %d, want 200", response.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("request status is not JSON: %v", err)
	}
	if body["request_id"] != "req_known" || body["state"] != "running" {
		t.Fatalf("request status body = %#v", body)
	}
}

func TestExecuteCancelsRuntimeWhenClientDisconnects(t *testing.T) {
	blocking := &blockingRuntime{started: make(chan struct{}), cancelled: make(chan struct{})}
	server := httptest.NewServer(New(Config{Runtime: blocking, Token: "test-token"}).Handler())
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/execute", strings.NewReader(`{"mode":"read","sql":"SELECT 1"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	closeResponseBody(t, response)
	select {
	case <-blocking.started:
	case <-time.After(5 * time.Second):
		t.Fatal("runtime did not start")
	}
	cancel()
	select {
	case <-blocking.cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("runtime did not receive client cancellation")
	}
}

func TestShutdownCancelsActiveRuntimeThroughCallback(t *testing.T) {
	runtime := &shutdownRuntime{started: make(chan struct{}), cancelled: make(chan struct{})}
	shutdownErr := make(chan error, 1)
	server := httptest.NewServer(New(Config{
		Runtime: runtime,
		Token:   "test-token",
		Shutdown: func() {
			shutdownErr <- runtime.Close(context.Background())
		},
	}).Handler())
	defer server.Close()
	headers := map[string]string{"Authorization": "Bearer test-token", "Content-Type": "application/json"}
	req, err := http.NewRequest(http.MethodPost, server.URL+"/execute", strings.NewReader(`{"mode":"read","sql":"SELECT 1"}`))
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	closeResponseBody(t, response)
	select {
	case <-runtime.started:
	case <-time.After(5 * time.Second):
		t.Fatal("runtime did not start")
	}
	shutdownReq, err := http.NewRequest(http.MethodPost, server.URL+"/shutdown", nil)
	if err != nil {
		t.Fatal(err)
	}
	shutdownReq.Header.Set("Authorization", "Bearer test-token")
	shutdownResponse, err := http.DefaultClient.Do(shutdownReq)
	if err != nil {
		t.Fatal(err)
	}
	closeResponseBody(t, shutdownResponse)
	if shutdownResponse.StatusCode != http.StatusAccepted {
		t.Fatalf("shutdown status = %d, want 202", shutdownResponse.StatusCode)
	}
	select {
	case <-runtime.cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown callback did not cancel runtime")
	}
	if err := <-shutdownErr; err != nil {
		t.Fatalf("shutdown runtime Close() error = %v", err)
	}
}

func TestExecuteStreamsSQLFailureFromRealRuntime(t *testing.T) {
	extDir := os.Getenv("GEODATA_SERVE_EXTENSION_DIR")
	if extDir == "" {
		t.Skip("set GEODATA_SERVE_EXTENSION_DIR to a directory containing DuckDB spatial and httpfs extensions")
	}
	root := t.TempDir()
	rt, err := runtime.New(context.Background(), runtime.Config{
		DatabasePath: filepath.Join(root, "data.duckdb"),
		RuntimeDir:   filepath.Join(root, "runtime"),
		BackupDir:    filepath.Join(root, "backups"),
		WorkingDir:   root,
		ExtensionDir: extDir,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	closeRuntime(t, rt)
	server := httptest.NewServer(New(Config{Runtime: rt, Token: "test-token"}).Handler())
	defer server.Close()
	request, err := http.NewRequest(http.MethodPost, server.URL+"/execute", strings.NewReader(`{"mode":"read","sql":"SELECT * FROM missing_table"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	closeResponseBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 after stream begins", response.StatusCode)
	}
	lines := strings.Split(strings.TrimSpace(readResponseBody(t, response)), "\n")
	if len(lines) != 3 {
		t.Fatalf("event count = %d, want queued/running/error", len(lines))
	}
	var terminal map[string]any
	if err := json.Unmarshal([]byte(lines[2]), &terminal); err != nil {
		t.Fatalf("invalid terminal event: %v", err)
	}
	if terminal["type"] != "error" || terminal["code"] != "sql_failed" || terminal["state"] != "failed" {
		t.Fatalf("terminal event = %#v", terminal)
	}
}

func TestExecuteStreamsJSONPrecisionFailureFromRealRuntime(t *testing.T) {
	extDir := os.Getenv("GEODATA_SERVE_EXTENSION_DIR")
	if extDir == "" {
		t.Skip("set GEODATA_SERVE_EXTENSION_DIR to a directory containing DuckDB spatial and httpfs extensions")
	}
	root := t.TempDir()
	rt, err := runtime.New(context.Background(), runtime.Config{
		DatabasePath: filepath.Join(root, "data.duckdb"),
		RuntimeDir:   filepath.Join(root, "runtime"),
		BackupDir:    filepath.Join(root, "backups"),
		WorkingDir:   root,
		ExtensionDir: extDir,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	closeRuntime(t, rt)
	server := httptest.NewServer(New(Config{Runtime: rt, Token: "test-token"}).Handler())
	defer server.Close()
	request, err := http.NewRequest(http.MethodPost, server.URL+"/execute", strings.NewReader(`{"mode":"read","sql":"SELECT '{\"large\":9007199254740993}'::JSON"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	closeResponseBody(t, response)
	lines := strings.Split(strings.TrimSpace(readResponseBody(t, response)), "\n")
	if len(lines) != 4 {
		t.Fatalf("event count = %d, want queued/running/schema/error", len(lines))
	}
	var terminal map[string]any
	if err := json.Unmarshal([]byte(lines[3]), &terminal); err != nil {
		t.Fatalf("invalid terminal event: %v", err)
	}
	if terminal["type"] != "error" || terminal["code"] != "result_encoding_failed" {
		t.Fatalf("terminal event = %#v, want result_encoding_failed", terminal)
	}
}

func TestExecuteCancelsRealDuckDBRequestWhenClientDisconnects(t *testing.T) {
	extDir := os.Getenv("GEODATA_SERVE_EXTENSION_DIR")
	if extDir == "" {
		t.Skip("set GEODATA_SERVE_EXTENSION_DIR to a directory containing DuckDB spatial and httpfs extensions")
	}
	root := t.TempDir()
	rt, err := runtime.New(context.Background(), runtime.Config{
		DatabasePath: filepath.Join(root, "data.duckdb"),
		RuntimeDir:   filepath.Join(root, "runtime"),
		BackupDir:    filepath.Join(root, "backups"),
		WorkingDir:   root,
		ExtensionDir: extDir,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	closeRuntime(t, rt)
	server := httptest.NewServer(New(Config{Runtime: rt, Token: "test-token"}).Handler())
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/execute", strings.NewReader(`{"mode":"read","sql":"SELECT sum(random()) FROM range(1000000000)"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	requestID := contract.RequestID(response.Header.Get("X-Request-ID"))
	if requestID == "" {
		t.Fatal("response omitted X-Request-ID")
	}
	waitForRuntimeState(t, rt, requestID, "running")
	cancel()
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close disconnected response body: %v", err)
	}
	waitForRuntimeState(t, rt, requestID, "cancelled")
}

func TestExecuteStreamsDeadlineExceededFromRealRuntime(t *testing.T) {
	extDir := os.Getenv("GEODATA_SERVE_EXTENSION_DIR")
	if extDir == "" {
		t.Skip("set GEODATA_SERVE_EXTENSION_DIR to a directory containing DuckDB spatial and httpfs extensions")
	}
	root := t.TempDir()
	rt, err := runtime.New(context.Background(), runtime.Config{
		DatabasePath: filepath.Join(root, "data.duckdb"),
		RuntimeDir:   filepath.Join(root, "runtime"),
		BackupDir:    filepath.Join(root, "backups"),
		WorkingDir:   root,
		ExtensionDir: extDir,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	closeRuntime(t, rt)
	server := httptest.NewServer(New(Config{Runtime: rt, Token: "test-token"}).Handler())
	defer server.Close()
	request, err := http.NewRequest(http.MethodPost, server.URL+"/execute", strings.NewReader(`{"mode":"read","sql":"SELECT sum(random()) FROM range(1000000000)","timeout_seconds":1}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	closeResponseBody(t, response)
	lines := strings.Split(strings.TrimSpace(readResponseBody(t, response)), "\n")
	if len(lines) != 3 {
		t.Fatalf("event count = %d, want queued/running/error", len(lines))
	}
	var terminal map[string]any
	if err := json.Unmarshal([]byte(lines[2]), &terminal); err != nil {
		t.Fatalf("invalid terminal event: %v", err)
	}
	if terminal["type"] != "error" || terminal["code"] != "deadline_exceeded" || terminal["state"] != "cancelled" {
		t.Fatalf("terminal event = %#v", terminal)
	}
}

func readResponseBody(t *testing.T, response *http.Response) string {
	t.Helper()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func waitForRuntimeState(t *testing.T, rt *runtime.RuntimeModule, requestID contract.RequestID, want string) {
	t.Helper()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if status, ok := rt.Status(requestID); ok && status.State == want {
			return
		}
		select {
		case <-timeout.C:
			status, _ := rt.Status(requestID)
			t.Fatalf("request %s state = %q, want %q", requestID, status.State, want)
		case <-ticker.C:
		}
	}
}
