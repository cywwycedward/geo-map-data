package httpserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"services/geodata-serve/internal/contract"
)

type Config struct {
	Runtime        contract.Runtime
	Token          string
	ServiceVersion string
	DuckDBVersion  string
	PID            int
	Shutdown       func()
}

type Server struct {
	runtime        contract.Runtime
	token          string
	serviceVersion string
	duckDBVersion  string
	pid            int
	shutdown       func()
	closing        atomic.Bool
	http           *http.Server
}

func New(config Config) *Server {
	if config.ServiceVersion == "" {
		config.ServiceVersion = contract.ServiceVersion
	}
	if config.DuckDBVersion == "" {
		config.DuckDBVersion = contract.DuckDBVersion
	}
	s := &Server{
		runtime:        config.Runtime,
		token:          config.Token,
		serviceVersion: config.ServiceVersion,
		duckDBVersion:  config.DuckDBVersion,
		pid:            config.PID,
		shutdown:       config.Shutdown,
	}
	s.http = &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       60 * time.Second,
	}
	return s
}

func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.serveHTTP) }

func (s *Server) Serve(listener net.Listener) error { return s.http.Serve(listener) }

func (s *Server) BeginShutdown() { s.closing.Store(true) }

func (s *Server) Shutdown(ctx context.Context) error {
	s.BeginShutdown()
	return s.http.Shutdown(ctx)
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/health":
		if r.Method != http.MethodGet {
			if !s.authorized(r) {
				writeError(w, http.StatusUnauthorized, "unauthorized", "authorization required")
				return
			}
			s.methodNotAllowed(w, http.MethodGet)
			return
		}
		s.health(w)
	case r.URL.Path == "/execute":
		s.execute(w, r)
	case strings.HasPrefix(r.URL.Path, "/requests/"):
		s.requestStatus(w, r)
	case r.URL.Path == "/shutdown":
		s.shutdownRequest(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) health(w http.ResponseWriter) {
	if s.closing.Load() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status":            "shutting_down",
			"interface_version": contract.InterfaceVersion,
			"service_version":   s.serviceVersion,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":            "ok",
		"interface_version": contract.InterfaceVersion,
		"service_version":   s.serviceVersion,
		"duckdb_version":    s.duckDBVersion,
		"pid":               s.pid,
	})
}

func (s *Server) execute(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authorization required")
		return
	}
	if r.Method != http.MethodPost {
		s.methodNotAllowed(w, http.MethodPost)
		return
	}
	if s.closing.Load() {
		writeError(w, http.StatusConflict, "service_shutting_down", "service is shutting down")
		return
	}
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
		return
	}
	var request executeRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON request")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_request", "request must contain one JSON object")
		return
	}
	if request.Mode != string(contract.ModeRead) && request.Mode != string(contract.ModeWrite) {
		writeError(w, http.StatusBadRequest, "invalid_request", "mode must be read or write")
		return
	}
	if strings.TrimSpace(request.SQL) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "sql must be non-empty")
		return
	}
	if request.TimeoutSeconds != nil && *request.TimeoutSeconds <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "timeout_seconds must be a positive integer")
		return
	}
	if request.TimeoutSeconds != nil && time.Duration(*request.TimeoutSeconds) > time.Duration(math.MaxInt64)/time.Second {
		writeError(w, http.StatusBadRequest, "invalid_request", "timeout_seconds is too large")
		return
	}
	requestID, err := contract.NewRequestID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not create request ID")
		return
	}
	w.Header().Set("X-Request-ID", string(requestID))
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal_error", "streaming responses are not supported")
		return
	}
	ctx := r.Context()
	if request.TimeoutSeconds != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(*request.TimeoutSeconds)*time.Second)
		defer cancel()
	}
	terminalWritten := false
	streamStarted := false
	sink := func(event contract.Event) error {
		if event.RequestID == "" {
			event.RequestID = requestID
		}
		streamStarted = true
		if err := writeEvent(w, event); err != nil {
			return err
		}
		if event.Type == "summary" || event.Type == "error" {
			terminalWritten = true
		}
		flusher.Flush()
		return nil
	}
	if err := s.runtime.Execute(ctx, contract.Command{ID: requestID, Mode: contract.Mode(request.Mode), SQL: request.SQL}, sink); err != nil && !streamStarted {
		if errors.Is(err, contract.ErrShuttingDown) {
			writeError(w, http.StatusConflict, "service_shutting_down", "service is shutting down")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "request failed")
		return
	} else if err != nil && !terminalWritten && r.Context().Err() == nil {
		_ = writeEvent(w, contract.Event{Type: "error", RequestID: requestID, State: "failed", Code: "internal_error", Message: "request failed"})
		flusher.Flush()
	}
}

func (s *Server) requestStatus(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authorization required")
		return
	}
	if r.Method != http.MethodGet {
		s.methodNotAllowed(w, http.MethodGet)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/requests/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "request_not_found", "request not found")
		return
	}
	status, ok := s.runtime.Status(contract.RequestID(id))
	if !ok {
		writeError(w, http.StatusNotFound, "request_not_found", "request not found")
		return
	}
	response := map[string]any{
		"request_id":  string(status.RequestID),
		"mode":        string(status.Mode),
		"state":       status.State,
		"accepted_at": status.AcceptedAt,
		"started_at":  nullableTime(status.StartedAt),
		"finished_at": nullableTime(status.FinishedAt),
		"row_count":   status.RowCount,
		"error_code":  nullableString(status.ErrorCode),
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) shutdownRequest(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authorization required")
		return
	}
	if r.Method != http.MethodPost {
		s.methodNotAllowed(w, http.MethodPost)
		return
	}
	if r.Body != nil {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil || strings.TrimSpace(string(body)) != "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "shutdown request body must be empty")
			return
		}
	}
	first := s.closing.CompareAndSwap(false, true)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "shutting_down"})
	if first && s.shutdown != nil {
		go s.shutdown()
	}
}

func (s *Server) authorized(r *http.Request) bool {
	const prefix = "Bearer "
	authorization := r.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, prefix) || s.token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(authorization[len(prefix):]), []byte(s.token)) == 1
}

func (s *Server) methodNotAllowed(w http.ResponseWriter, method string) {
	w.Header().Set("Allow", method)
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}

type executeRequest struct {
	Mode           string `json:"mode"`
	SQL            string `json:"sql"`
	TimeoutSeconds *int   `json:"timeout_seconds"`
}

func writeEvent(w http.ResponseWriter, event contract.Event) error {
	var wire any
	switch event.Type {
	case "status":
		wire = map[string]any{"type": event.Type, "request_id": string(event.RequestID), "state": event.State, "at": event.At}
	case "schema":
		columns := make([]map[string]string, len(event.Columns))
		for i, column := range event.Columns {
			columns[i] = map[string]string{"name": column.Name, "duckdb_type": column.DuckDBType}
		}
		wire = map[string]any{"type": event.Type, "request_id": string(event.RequestID), "columns": columns}
	case "row":
		wire = map[string]any{"type": event.Type, "request_id": string(event.RequestID), "values": event.Values}
	case "summary":
		wire = map[string]any{"type": event.Type, "request_id": string(event.RequestID), "state": event.State, "row_count": event.RowCount, "queued_ms": event.QueuedMS, "execution_ms": event.ExecutionMS}
	case "error":
		wire = map[string]any{"type": event.Type, "request_id": string(event.RequestID), "state": event.State, "code": event.Code, "message": event.Message, "queued_ms": event.QueuedMS, "execution_ms": event.ExecutionMS}
	default:
		return errors.New("unknown runtime event")
	}
	return json.NewEncoder(w).Encode(wire)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func isJSONContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
