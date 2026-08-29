package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"services/geodata-serve/internal/bootstrap"
)

func TestServeShutdownCancelsRuntimeBeforeStoppingHTTP(t *testing.T) {
	extDir := os.Getenv("GEODATA_SERVE_EXTENSION_DIR")
	if extDir == "" {
		t.Skip("set GEODATA_SERVE_EXTENSION_DIR to a directory containing DuckDB spatial and httpfs extensions")
	}
	root := t.TempDir()
	runtimeDir := filepath.Join(root, "runtime")
	linkExtensions(t, extDir, filepath.Join(runtimeDir, "extensions"))
	done := make(chan error, 1)
	go func() {
		done <- runServe([]string{
			"--database", filepath.Join(root, "data.duckdb"),
			"--runtime-dir", runtimeDir,
			"--backup-dir", filepath.Join(root, "backups"),
			"--working-dir", root,
		})
	}()
	state := waitForServerState(t, filepath.Join(runtimeDir, "server.json"))
	request, err := http.NewRequest(http.MethodPost, state.Address+"/execute", strings.NewReader(`{"mode":"read","sql":"SELECT sum(random()) FROM range(1000000000)"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+state.Token)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	requestID := response.Header.Get("X-Request-ID")
	if requestID == "" {
		t.Fatal("execute response omitted X-Request-ID")
	}
	waitForRequestState(t, state, requestID, "running")
	shutdownRequest, err := http.NewRequest(http.MethodPost, state.Address+"/shutdown", nil)
	if err != nil {
		t.Fatal(err)
	}
	shutdownRequest.Header.Set("Authorization", "Bearer "+state.Token)
	shutdownResponse, err := http.DefaultClient.Do(shutdownRequest)
	if err != nil {
		t.Fatal(err)
	}
	if shutdownResponse.StatusCode != http.StatusAccepted {
		t.Fatalf("shutdown status = %d, want 202", shutdownResponse.StatusCode)
	}
	if err := shutdownResponse.Body.Close(); err != nil {
		t.Fatalf("close shutdown response: %v", err)
	}
	body, readErr := io.ReadAll(response.Body)
	if closeErr := response.Body.Close(); closeErr != nil {
		readErr = errors.Join(readErr, closeErr)
	}
	if readErr != nil {
		t.Fatalf("read cancelled execute response: %v", readErr)
	}
	if !strings.Contains(string(body), `"state":"cancelled"`) {
		t.Fatalf("execute response = %s, want cancelled terminal event", body)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runServe() error = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runServe did not return after shutdown")
	}
	if _, err := os.Stat(filepath.Join(runtimeDir, "server.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("server state after shutdown error = %v, want removed", err)
	}
}

func TestInitReportsActualSpatialVersion(t *testing.T) {
	extensions := os.Getenv("GEODATA_SERVE_EXTENSION_DIR")
	if extensions == "" {
		t.Skip("set GEODATA_SERVE_EXTENSION_DIR to a directory containing DuckDB spatial and httpfs extensions")
	}
	runtimeDir := t.TempDir()
	linkExtensions(t, extensions, filepath.Join(runtimeDir, "extensions"))
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = writer
	err = runInit([]string{"--runtime-dir", runtimeDir})
	if closeErr := writer.Close(); closeErr != nil {
		err = errors.Join(err, closeErr)
	}
	os.Stdout = stdout
	if err != nil {
		t.Fatalf("runInit() error = %v", err)
	}
	output, err := io.ReadAll(reader)
	if closeErr := reader.Close(); closeErr != nil {
		err = errors.Join(err, closeErr)
	}
	if err != nil {
		t.Fatalf("read init output: %v", err)
	}
	var response struct {
		SpatialVersion string `json:"spatial_version"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatalf("decode init output: %v: %s", err, output)
	}
	if response.SpatialVersion == "" {
		t.Fatalf("init response = %s, want actual spatial_version", output)
	}
}

func waitForServerState(t *testing.T, path string) bootstrap.ServerState {
	t.Helper()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := bootstrap.ReadServerState(path)
		if err == nil {
			return state
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read server state: %v", err)
		}
		select {
		case <-timeout.C:
			t.Fatal("server state was not created")
		case <-ticker.C:
		}
	}
}

func waitForRequestState(t *testing.T, state bootstrap.ServerState, requestID, want string) {
	t.Helper()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, state.Address+"/requests/"+requestID, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+state.Token)
		response, err := http.DefaultClient.Do(request)
		if err == nil {
			var status struct {
				State string `json:"state"`
			}
			decodeErr := json.NewDecoder(response.Body).Decode(&status)
			closeErr := response.Body.Close()
			if decodeErr != nil || closeErr != nil {
				t.Fatalf("read request status: %v", errors.Join(decodeErr, closeErr))
			}
			if status.State == want {
				return
			}
		}
		select {
		case <-timeout.C:
			t.Fatalf("request %s did not reach state %q", requestID, want)
		case <-ticker.C:
		}
	}
}

func linkExtensions(t *testing.T, source, target string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatalf("create test runtime directory: %v", err)
	}
	if err := os.Symlink(source, target); err == nil {
		t.Cleanup(func() {
			if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Errorf("remove prepared-extension link: %v", err)
			}
		})
		return
	}
	output, err := exec.Command("cmd.exe", "/c", "mklink", "/J", target, source).CombinedOutput()
	if err != nil {
		t.Fatalf("create prepared-extension junction: %v: %s", err, output)
	}
	t.Cleanup(func() {
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("remove prepared-extension link: %v", err)
		}
	})
}
