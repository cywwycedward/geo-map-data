package runtime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"services/geodata-serve/internal/backup"
	"services/geodata-serve/internal/bootstrap"
	"services/geodata-serve/internal/contract"
	"services/geodata-serve/internal/duckdbconn"
)

const (
	ServiceVersion   = contract.ServiceVersion
	InterfaceVersion = contract.InterfaceVersion
	DuckDBVersion    = contract.DuckDBVersion
)

type Mode = contract.Mode
type RequestID = contract.RequestID
type Command = contract.Command
type Column = contract.Column
type Event = contract.Event
type EventSink = contract.EventSink
type RequestStatus = contract.RequestStatus

const (
	ModeRead  = contract.ModeRead
	ModeWrite = contract.ModeWrite
)

type Config struct {
	// Paths is the canonical service configuration produced by bootstrap.
	Paths bootstrap.Paths
	// The individual fields remain accepted for package compatibility with
	// existing callers; serve passes Paths so runtime layout cannot diverge.
	DatabasePath string
	RuntimeDir   string
	BackupDir    string
	WorkingDir   string
	// ExtensionDir and TempDir are legacy test/bootstrap overrides. They are
	// ignored when Paths is set and are not used by the CLI.
	ExtensionDir string
	TempDir      string
	Logger       *slog.Logger
}

var (
	ErrShuttingDown   = contract.ErrShuttingDown
	ErrInvalidCommand = contract.ErrInvalidCommand
	ErrBackupFailed   = contract.ErrBackupFailed
	ErrResultEncoding = contract.ErrResultEncoding

	errorMessageURLCredentials = regexp.MustCompile(`(://[^/\s:@]+:)[^@/\s]+@`)
	errorMessageAssignment     = regexp.MustCompile(`(?i)\b(access[_-]?key|api[_-]?key|authorization|password|passwd|secret|token)\s*=\s*[^\s,&;]+`)
	errorMessageBearer         = regexp.MustCompile(`(?i)\bbearer\s+[^\s,;]+`)
)

type Runtime = contract.Runtime

type RuntimeModule struct {
	db             *sql.DB
	dbOwner        *duckdbconn.Handle
	spatialVersion string
	logger         *slog.Logger
	backupStore    *backup.Store

	shutdownDone  chan struct{}
	readSlots     chan struct{}
	writeWake     chan struct{}
	writeMu       sync.Mutex
	writeQueue    []*requestTask
	workerDone    chan struct{}
	activeWG      sync.WaitGroup
	activeCancels map[RequestID]context.CancelFunc

	mu      sync.RWMutex
	states  map[RequestID]RequestStatus
	closing bool

	shutdownOnce sync.Once
	requestsDone chan struct{}
	closeOnce    sync.Once
	closeDone    chan struct{}
	closeMu      sync.Mutex
	closeErr     error
}

type requestTask struct {
	command      Command
	sink         EventSink
	ready        chan struct{}
	done         chan error
	cancellation <-chan struct{}
	hasDeadline  bool
	deadline     time.Time

	writeMu           sync.Mutex
	writeStarted      bool
	writeCancelled    bool
	writeCancel       context.CancelFunc
	cancellationCause error
	readyOnce         sync.Once
}

func New(ctx context.Context, config Config) (*RuntimeModule, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	paths, extensionDir, tempDir, err := canonicalPaths(config)
	if err != nil {
		return nil, err
	}
	if err := paths.EnsureDirectories(); err != nil {
		return nil, err
	}
	if config.ExtensionDir != "" && config.Paths.RuntimeDir == "" {
		if err := os.MkdirAll(extensionDir, 0o700); err != nil {
			return nil, fmt.Errorf("create extension directory: %w", err)
		}
	}
	owner, err := duckdbconn.Open(ctx, duckdbconn.Config{
		DatabasePath:   paths.Database,
		ExtensionDir:   extensionDir,
		TempDir:        tempDir,
		WorkingDir:     paths.WorkingDir,
		MaxOpenConns:   3,
		LoadExtensions: true,
	})
	if err != nil {
		return nil, err
	}
	db := owner.DB
	spatialVersion, err := loadedExtensionVersion(ctx, db, "spatial")
	if err != nil {
		return nil, errors.Join(fmt.Errorf("read spatial extension version: %w", err), owner.Close())
	}
	backupStore, err := backup.NewStore(backup.Config{
		BackupDir:    paths.BackupDir,
		ExtensionDir: extensionDir,
		TempDir:      tempDir,
		WorkingDir:   paths.WorkingDir,
	})
	if err != nil {
		return nil, errors.Join(err, owner.Close())
	}

	r := &RuntimeModule{
		db:             db,
		dbOwner:        owner,
		spatialVersion: spatialVersion,
		backupStore:    backupStore,
		logger:         config.Logger,
		shutdownDone:   make(chan struct{}),
		readSlots:      make(chan struct{}, 2),
		writeWake:      make(chan struct{}, 1),
		workerDone:     make(chan struct{}),
		activeCancels:  make(map[RequestID]context.CancelFunc),
		states:         make(map[RequestID]RequestStatus),
		requestsDone:   make(chan struct{}),
		closeDone:      make(chan struct{}),
	}
	if r.logger == nil {
		r.logger = slog.Default()
	}
	go r.writeWorker()
	return r, nil
}

func loadedExtensionVersion(ctx context.Context, db *sql.DB, extension string) (string, error) {
	var version string
	err := db.QueryRowContext(ctx, "SELECT extension_version FROM duckdb_extensions() WHERE extension_name = ?", extension).Scan(&version)
	if err != nil {
		return "", err
	}
	if version == "" {
		return "", errors.New("extension version is empty")
	}
	return version, nil
}

func canonicalPaths(config Config) (bootstrap.Paths, string, string, error) {
	if config.Paths.Database != "" {
		paths := config.Paths
		if paths.RuntimeLayout.RuntimeDir == "" {
			layout, err := bootstrap.ResolveRuntimeLayout(paths.RuntimeDir)
			if err != nil {
				return bootstrap.Paths{}, "", "", err
			}
			paths.RuntimeLayout = layout
		}
		if paths.BackupDir == "" {
			return bootstrap.Paths{}, "", "", errors.New("backup path is required")
		}
		return paths, paths.ExtensionsDir(), paths.DuckDBTempDir(), nil
	}
	if config.DatabasePath == "" || config.RuntimeDir == "" || config.BackupDir == "" {
		return bootstrap.Paths{}, "", "", errors.New("database, runtime, and backup paths are required")
	}
	databasePath, err := absolutePath(config.DatabasePath)
	if err != nil {
		return bootstrap.Paths{}, "", "", fmt.Errorf("database path: %w", err)
	}
	runtimeDir, err := absolutePath(config.RuntimeDir)
	if err != nil {
		return bootstrap.Paths{}, "", "", fmt.Errorf("runtime path: %w", err)
	}
	backupDir, err := absolutePath(config.BackupDir)
	if err != nil {
		return bootstrap.Paths{}, "", "", fmt.Errorf("backup path: %w", err)
	}
	workingDir := ""
	if config.WorkingDir != "" {
		workingDir, err = absolutePath(config.WorkingDir)
		if err != nil {
			return bootstrap.Paths{}, "", "", fmt.Errorf("working path: %w", err)
		}
		info, statErr := os.Stat(workingDir)
		if statErr != nil {
			return bootstrap.Paths{}, "", "", fmt.Errorf("working path: %w", statErr)
		}
		if !info.IsDir() {
			return bootstrap.Paths{}, "", "", errors.New("working path is not a directory")
		}
	}
	layout, err := bootstrap.ResolveRuntimeLayout(runtimeDir)
	if err != nil {
		return bootstrap.Paths{}, "", "", err
	}
	extensionDir := layout.ExtensionsDir
	if config.ExtensionDir != "" {
		extensionDir, err = absolutePath(config.ExtensionDir)
		if err != nil {
			return bootstrap.Paths{}, "", "", fmt.Errorf("extension path: %w", err)
		}
	}
	tempDir := layout.DuckDBTempDir
	if config.TempDir != "" {
		tempDir, err = absolutePath(config.TempDir)
		if err != nil {
			return bootstrap.Paths{}, "", "", fmt.Errorf("temp path: %w", err)
		}
	}
	return bootstrap.Paths{Database: databasePath, RuntimeDir: runtimeDir, BackupDir: backupDir, WorkingDir: workingDir, RuntimeLayout: layout}, extensionDir, tempDir, nil
}

func absolutePath(path string) (string, error) {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func (r *RuntimeModule) Execute(ctx context.Context, command Command, sink EventSink) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if command.ID == "" || (command.Mode != ModeRead && command.Mode != ModeWrite) || strings.TrimSpace(command.SQL) == "" {
		return ErrInvalidCommand
	}
	if sink == nil {
		sink = func(Event) error { return nil }
	}

	runCtx, cancel := context.WithCancel(ctx)
	task := newRequestTask(runCtx, command, sink)
	acceptedAt := time.Now().UTC()
	r.mu.Lock()
	if r.closing {
		r.mu.Unlock()
		cancel()
		return ErrShuttingDown
	}
	if _, exists := r.states[command.ID]; exists {
		r.mu.Unlock()
		cancel()
		return ErrInvalidCommand
	}
	r.states[command.ID] = RequestStatus{RequestID: command.ID, Mode: command.Mode, State: "queued", AcceptedAt: acceptedAt}
	r.activeCancels[command.ID] = cancel
	r.activeWG.Add(1)
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.activeCancels, command.ID)
		r.mu.Unlock()
		cancel()
		r.activeWG.Done()
	}()
	if command.Mode == ModeRead {
		if err := sink(Event{Type: "status", RequestID: command.ID, State: "queued", At: acceptedAt}); err != nil {
			cancel()
			r.markCancelled(task, context.Canceled)
			return err
		}
		return r.executeRead(runCtx, cancel, task)
	}
	if !r.enqueueWrite(task) {
		return r.finishCancelled(task, context.Canceled)
	}
	if err := sink(Event{Type: "status", RequestID: command.ID, State: "queued", At: acceptedAt}); err != nil {
		cancel()
		task.cancelWrite(context.Canceled)
		task.releaseWrite()
		r.markCancelled(task, context.Canceled)
		return err
	}
	stopCancellation := context.AfterFunc(runCtx, func() {
		if task.cancelWrite(runCtx.Err()) {
			task.done <- r.finishCancelled(task, task.cancellationError(context.Canceled))
		}
	})
	defer stopCancellation()
	task.releaseWrite()
	return <-task.done
}

func newRequestTask(ctx context.Context, command Command, sink EventSink) *requestTask {
	task := &requestTask{command: command, sink: sink, done: make(chan error, 1), cancellation: ctx.Done()}
	if command.Mode == ModeWrite {
		task.ready = make(chan struct{})
	}
	if deadline, ok := ctx.Deadline(); ok {
		task.deadline = deadline
		task.hasDeadline = true
	}
	return task
}

func (r *RuntimeModule) executeRead(ctx context.Context, cancel context.CancelFunc, task *requestTask) error {
	select {
	case r.readSlots <- struct{}{}:
		defer func() { <-r.readSlots }()
	case <-ctx.Done():
		return r.finishCancelled(task, ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return r.finishCancelled(task, err)
	}
	return r.executeSQL(ctx, cancel, task, false)
}

func (r *RuntimeModule) writeWorker() {
	defer close(r.workerDone)
	for {
		task, stopping := r.nextWrite()
		if stopping {
			return
		}
		select {
		case <-task.ready:
		case <-r.shutdownDone:
			return
		}
		executeCtx, executeCancel, started := task.startWrite()
		if !started {
			continue
		}
		err := r.executeWrite(executeCtx, executeCancel, task)
		executeCancel()
		task.done <- err
	}
}

func (task *requestTask) releaseWrite() {
	task.readyOnce.Do(func() { close(task.ready) })
}

func (task *requestTask) startWrite() (context.Context, context.CancelFunc, bool) {
	task.writeMu.Lock()
	defer task.writeMu.Unlock()
	if task.writeCancelled {
		return nil, nil, false
	}
	select {
	case <-task.cancellation:
		return nil, nil, false
	default:
	}
	var ctx context.Context
	var cancel context.CancelFunc
	if task.hasDeadline {
		ctx, cancel = context.WithDeadline(context.Background(), task.deadline)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	task.writeStarted = true
	task.writeCancel = cancel
	return ctx, cancel, true
}

func (task *requestTask) cancelWrite(cause error) bool {
	task.writeMu.Lock()
	if task.writeCancelled {
		task.writeMu.Unlock()
		return false
	}
	task.writeCancelled = true
	task.cancellationCause = cause
	queued := !task.writeStarted
	cancel := task.writeCancel
	task.writeMu.Unlock()
	if cancel != nil {
		cancel()
	}
	return queued
}

func (task *requestTask) cancellationError(fallback error) error {
	task.writeMu.Lock()
	cause := task.cancellationCause
	task.writeMu.Unlock()
	if cause != nil {
		return cause
	}
	return fallback
}

func (r *RuntimeModule) enqueueWrite(task *requestTask) bool {
	r.mu.Lock()
	if r.closing {
		r.mu.Unlock()
		return false
	}
	r.writeMu.Lock()
	r.writeQueue = append(r.writeQueue, task)
	r.writeMu.Unlock()
	r.mu.Unlock()
	select {
	case r.writeWake <- struct{}{}:
	default:
	}
	return true
}

func (r *RuntimeModule) nextWrite() (*requestTask, bool) {
	for {
		r.writeMu.Lock()
		if len(r.writeQueue) > 0 {
			task := r.writeQueue[0]
			r.writeQueue = r.writeQueue[1:]
			r.writeMu.Unlock()
			return task, false
		}
		r.writeMu.Unlock()
		select {
		case <-r.writeWake:
		case <-r.shutdownDone:
			return nil, true
		}
	}
}

func (r *RuntimeModule) executeWrite(ctx context.Context, cancel context.CancelFunc, task *requestTask) error {
	if err := ctx.Err(); err != nil {
		return r.finishCancelled(task, task.cancellationError(err))
	}
	if err := r.emitStatus(task, "backing_up"); err != nil {
		cancel()
		return r.finishCancelled(task, err)
	}
	if err := r.createBackup(ctx, task.command.ID); err != nil {
		if ctx.Err() != nil {
			return r.finishCancelled(task, task.cancellationError(ctx.Err()))
		}
		return r.finishTerminal(task, "failed", "backup_failed", ErrBackupFailed, err)
	}
	return r.executeSQL(ctx, cancel, task, true)
}

func (r *RuntimeModule) executeSQL(ctx context.Context, cancel context.CancelFunc, task *requestTask, write bool) (err error) {
	if err = r.emitStatus(task, "running"); err != nil {
		cancel()
		return r.finishCancelled(task, err)
	}
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return r.finishTerminal(task, terminalState(ctx), errorCode(ctx, "sql_failed"), err, err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			r.logger.Error("connection_close_failed", "request_id", string(task.command.ID), "error", closeErr)
			if err == nil {
				err = closeErr
			}
		}
	}()

	rows, err := conn.QueryContext(ctx, task.command.SQL)
	if err != nil {
		if write {
			r.tryRollback(conn, task)
		}
		return r.finishTerminal(task, terminalState(ctx), errorCode(ctx, "sql_failed"), err, err)
	}
	rowsClosed := false
	closeRows := func() error {
		if rowsClosed {
			return nil
		}
		rowsClosed = true
		closeErr := rows.Close()
		if closeErr != nil {
			r.logger.Error("rows_close_failed", "request_id", string(task.command.ID), "error", closeErr)
		}
		return closeErr
	}
	defer func() {
		if closeErr := closeRows(); closeErr != nil {
			if err == nil {
				err = closeErr
			}
		}
	}()

	columns, err := rows.ColumnTypes()
	if err != nil {
		if closeErr := closeRows(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		if write {
			r.tryRollback(conn, task)
		}
		return r.finishTerminal(task, "failed", "result_encoding_failed", ErrResultEncoding, err)
	}
	columnEvents := make([]Column, len(columns))
	for i, column := range columns {
		columnEvents[i] = Column{Name: column.Name(), DuckDBType: column.DatabaseTypeName()}
	}
	if len(columnEvents) > 0 {
		if err := task.sink(Event{Type: "schema", RequestID: task.command.ID, Columns: columnEvents}); err != nil {
			cancel()
			if closeErr := closeRows(); closeErr != nil {
				r.logger.Error("rows_close_failed", "request_id", string(task.command.ID), "error", closeErr)
			}
			if write {
				r.tryRollback(conn, task)
			}
			r.markCancelled(task, context.Canceled)
			return err
		}
	}

	var rowCount int64
	for rows.Next() {
		raw := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range raw {
			dest[i] = &raw[i]
		}
		if err := rows.Scan(dest...); err != nil {
			if closeErr := closeRows(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
			if write {
				r.tryRollback(conn, task)
			}
			return r.finishTerminal(task, terminalState(ctx), errorCode(ctx, "result_encoding_failed"), ErrResultEncoding, err)
		}
		values := make([]any, len(raw))
		for i := range raw {
			values[i], err = encodeValue(raw[i], columns[i].DatabaseTypeName())
			if err != nil {
				if closeErr := closeRows(); closeErr != nil {
					err = errors.Join(err, closeErr)
				}
				if write {
					r.tryRollback(conn, task)
				}
				return r.finishTerminal(task, terminalState(ctx), errorCode(ctx, "result_encoding_failed"), ErrResultEncoding, err)
			}
		}
		if err := task.sink(Event{Type: "row", RequestID: task.command.ID, Values: values}); err != nil {
			cancel()
			if closeErr := closeRows(); closeErr != nil {
				r.logger.Error("rows_close_failed", "request_id", string(task.command.ID), "error", closeErr)
			}
			if write {
				r.tryRollback(conn, task)
			}
			r.markCancelled(task, context.Canceled)
			return err
		}
		rowCount++
	}
	if err := rows.Err(); err != nil {
		if closeErr := closeRows(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		if write {
			r.tryRollback(conn, task)
		}
		return r.finishTerminal(task, terminalState(ctx), errorCode(ctx, "sql_failed"), err, err)
	}
	if err := ctx.Err(); err != nil {
		if closeErr := closeRows(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		if write {
			r.tryRollback(conn, task)
		}
		return r.finishCancelled(task, task.cancellationError(err))
	}

	var resultRows *int64
	if len(columns) > 0 {
		resultRows = &rowCount
	}
	queuedMS, executionMS := r.timings(task.command.ID, time.Now().UTC())
	return r.finishSuccess(task, resultRows, queuedMS, executionMS)
}

func (r *RuntimeModule) emitStatus(task *requestTask, state string) error {
	r.mu.Lock()
	r.states[task.command.ID] = r.transitionLocked(task.command.ID, state)
	r.mu.Unlock()
	return task.sink(Event{Type: "status", RequestID: task.command.ID, State: state, At: time.Now().UTC()})
}

func (r *RuntimeModule) finishSuccess(task *requestTask, rows *int64, queuedMS, executionMS int64) error {
	now := time.Now().UTC()
	r.mu.Lock()
	status := r.states[task.command.ID]
	status.State = "finished"
	status.FinishedAt = now
	status.RowCount = rows
	r.states[task.command.ID] = status
	r.mu.Unlock()
	r.logStatus(status, queuedMS, executionMS, "")
	return task.sink(Event{Type: "summary", RequestID: task.command.ID, State: "finished", RowCount: rows, QueuedMS: queuedMS, ExecutionMS: executionMS})
}

func (r *RuntimeModule) finishTerminal(task *requestTask, state, code string, returned, cause error) error {
	if state == "" {
		state = "failed"
	}
	if code == "" {
		code = "internal_error"
	}
	now := time.Now().UTC()
	r.mu.Lock()
	status := r.states[task.command.ID]
	status.State = state
	status.FinishedAt = now
	status.ErrorCode = code
	r.states[task.command.ID] = status
	r.mu.Unlock()
	queuedMS, executionMS := r.timings(task.command.ID, now)
	r.logStatus(status, queuedMS, executionMS, code)
	message := safeErrorMessage(cause)
	if state == "cancelled" {
		message = "request cancelled"
	}
	if sinkErr := task.sink(Event{Type: "error", RequestID: task.command.ID, State: state, Code: code, Message: message, QueuedMS: queuedMS, ExecutionMS: executionMS}); sinkErr != nil {
		return sinkErr
	}
	return returned
}

func (r *RuntimeModule) finishCancelled(task *requestTask, cause error) error {
	if cause == nil {
		cause = context.Canceled
	}
	code := errorCodeFor(cause, "cancelled")
	return r.finishTerminal(task, "cancelled", code, cause, cause)
}

func (r *RuntimeModule) markCancelled(task *requestTask, cause error) {
	now := time.Now().UTC()
	r.mu.Lock()
	status := r.states[task.command.ID]
	status.State = "cancelled"
	status.FinishedAt = now
	status.ErrorCode = errorCodeFor(cause, "cancelled")
	r.states[task.command.ID] = status
	r.mu.Unlock()
	r.logStatus(status, 0, 0, status.ErrorCode)
}

func (r *RuntimeModule) transitionLocked(id RequestID, state string) RequestStatus {
	status := r.states[id]
	status.State = state
	if (state == "backing_up" || state == "running") && status.StartedAt.IsZero() {
		status.StartedAt = time.Now().UTC()
	}
	return status
}

func (r *RuntimeModule) timings(id RequestID, executionEnd time.Time) (int64, int64) {
	r.mu.RLock()
	status := r.states[id]
	r.mu.RUnlock()
	queuedEnd := status.StartedAt
	if queuedEnd.IsZero() {
		queuedEnd = executionEnd
	}
	queuedMS := queuedEnd.Sub(status.AcceptedAt).Milliseconds()
	executionMS := executionEnd.Sub(queuedEnd).Milliseconds()
	if queuedMS < 0 {
		queuedMS = 0
	}
	if executionMS < 0 {
		executionMS = 0
	}
	return queuedMS, executionMS
}

func (r *RuntimeModule) Status(id RequestID) (RequestStatus, bool) {
	r.mu.RLock()
	status, ok := r.states[id]
	r.mu.RUnlock()
	return status, ok
}

func (r *RuntimeModule) SpatialVersion() string { return r.spatialVersion }

func (r *RuntimeModule) BeginShutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	r.beginShutdown()
	select {
	case <-r.requestsDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *RuntimeModule) beginShutdown() {
	r.shutdownOnce.Do(func() {
		r.mu.Lock()
		r.closing = true
		cancels := make([]context.CancelFunc, 0, len(r.activeCancels))
		for _, cancel := range r.activeCancels {
			cancels = append(cancels, cancel)
		}
		r.mu.Unlock()
		close(r.shutdownDone)
		for _, cancel := range cancels {
			cancel()
		}
		go func() {
			<-r.workerDone
			r.activeWG.Wait()
			close(r.requestsDone)
		}()
	})
}

func (r *RuntimeModule) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	r.beginShutdown()
	r.closeOnce.Do(func() {
		go func() {
			<-r.requestsDone
			dbErr := r.dbOwner.Close()
			r.closeMu.Lock()
			r.closeErr = dbErr
			r.closeMu.Unlock()
			close(r.closeDone)
		}()
	})
	select {
	case <-r.closeDone:
		r.closeMu.Lock()
		err := r.closeErr
		r.closeMu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *RuntimeModule) tryRollback(conn *sql.Conn, task *requestTask) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := conn.ExecContext(ctx, "ROLLBACK"); err != nil {
		r.logger.Error("rollback_failed",
			"request_id", string(task.command.ID),
			"mode", string(task.command.Mode),
			"state", "failed",
			"error_code", "sql_failed",
			"error", err,
		)
	}
}

func (r *RuntimeModule) logStatus(status RequestStatus, queuedMS, executionMS int64, code string) {
	attrs := []any{"request_id", string(status.RequestID), "mode", string(status.Mode), "state", status.State, "queued_ms", queuedMS, "execution_ms", executionMS, "error_code", code}
	if status.RowCount != nil {
		attrs = append(attrs, "row_count", *status.RowCount)
	}
	r.logger.Info("request_finished", attrs...)
}

func terminalState(ctx context.Context) string {
	if ctx != nil && ctx.Err() != nil {
		return "cancelled"
	}
	return "failed"
}

func errorCode(ctx context.Context, fallback string) string {
	if ctx == nil {
		return fallback
	}
	return errorCodeFor(ctx.Err(), fallback)
}

func errorCodeFor(err error, fallback string) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	if err != nil {
		return "cancelled"
	}
	return fallback
}

func safeErrorMessage(err error) string {
	if err == nil {
		return "request failed"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "request cancelled"
	}
	if errors.Is(err, ErrBackupFailed) {
		return "database backup failed"
	}
	if errors.Is(err, ErrResultEncoding) {
		return "result encoding failed"
	}
	return duckDBErrorSummary(err.Error())
}

func duckDBErrorSummary(message string) string {
	var parts []string
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "LINE ") || strings.HasPrefix(line, "^") || isSQLContextLine(line) {
			break
		}
		parts = append(parts, line)
	}

	summary := redactErrorSecrets(strings.Join(parts, " "))
	if summary == "" {
		return "DuckDB request failed"
	}
	return truncateErrorSummary(summary)
}

func isSQLContextLine(line string) bool {
	upper := strings.ToUpper(line)
	for _, prefix := range []string{"SELECT ", "INSERT ", "UPDATE ", "DELETE ", "CREATE ", "ALTER ", "DROP ", "COPY ", "WITH ", "FROM ", "SET ", "BEGIN", "COMMIT", "ROLLBACK"} {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

func redactErrorSecrets(message string) string {
	message = errorMessageURLCredentials.ReplaceAllString(message, "${1}[REDACTED]@")
	message = errorMessageAssignment.ReplaceAllStringFunc(message, func(match string) string {
		key, _, _ := strings.Cut(match, "=")
		return strings.TrimSpace(key) + "=[REDACTED]"
	})
	return errorMessageBearer.ReplaceAllString(message, "Bearer [REDACTED]")
}

func truncateErrorSummary(message string) string {
	const maxRunes = 512
	runes := []rune(message)
	if len(runes) <= maxRunes {
		return message
	}
	return string(runes[:maxRunes-1]) + "…"
}
