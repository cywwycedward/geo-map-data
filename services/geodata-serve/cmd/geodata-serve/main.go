package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	goruntime "runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"services/geodata-serve/internal/bootstrap"
	"services/geodata-serve/internal/httpserver"
	"services/geodata-serve/internal/restore"
	"services/geodata-serve/internal/runtime"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: geodata-serve version|init|serve|restore")
	}
	switch args[0] {
	case "version":
		if len(args) != 1 {
			return errors.New("version does not accept options")
		}
		fmt.Printf("geodata-serve %s (DuckDB %s)\n", runtime.ServiceVersion, runtime.DuckDBVersion)
		return nil
	case "init":
		return runInit(args[1:])
	case "serve":
		return runServe(args[1:])
	case "restore":
		return runRestore(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runInit(args []string) error {
	flags := newFlagSet("init")
	runtimeDir := flags.String("runtime-dir", "", "service runtime directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *runtimeDir == "" {
		return errors.New("--runtime-dir is required")
	}
	extensions, err := bootstrap.InstallExtensions(context.Background(), *runtimeDir)
	if err != nil {
		return err
	}
	layout, err := bootstrap.ResolveRuntimeLayout(*runtimeDir)
	if err != nil {
		return fmt.Errorf("runtime directory: %w", err)
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"status":          "ok",
		"service_version": runtime.ServiceVersion,
		"duckdb_version":  runtime.DuckDBVersion,
		"spatial_version": extensions.SpatialVersion,
		"platform":        goruntime.GOOS + "/" + goruntime.GOARCH,
		"extension_dir":   layout.ExtensionsDir,
		"extensions":      []string{"spatial", "httpfs"},
	})
}

func runServe(args []string) (err error) {
	flags := newFlagSet("serve")
	database := flags.String("database", "", "persistent DuckDB database file")
	runtimeDir := flags.String("runtime-dir", "", "service runtime directory")
	backupDir := flags.String("backup-dir", "", "complete backup directory")
	workingDir := flags.String("working-dir", "", "relative SQL path base directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	paths, err := bootstrap.ResolvePaths(bootstrap.PathOptions{Database: *database, RuntimeDir: *runtimeDir, BackupDir: *backupDir, WorkingDir: *workingDir})
	if err != nil {
		return err
	}
	if err := paths.EnsureDirectories(); err != nil {
		return err
	}
	restoreWorkingDir, err := bootstrap.EnterWorkingDirectory(paths.WorkingDir)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, restoreWorkingDir())
	}()
	if err := removeStaleState(paths.ServerStateFile()); err != nil {
		return err
	}
	logger := slog.Default().With(
		"service_version", runtime.ServiceVersion,
		"duckdb_version", runtime.DuckDBVersion,
	)
	rt, err := runtime.New(context.Background(), runtime.Config{
		Paths:  paths,
		Logger: logger,
	})
	if err != nil {
		return err
	}
	token, err := newToken()
	if err != nil {
		return errors.Join(err, rt.Close(context.Background()))
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return errors.Join(fmt.Errorf("listen on loopback: %w", err), rt.Close(context.Background()))
	}
	pid := os.Getpid()
	state := bootstrap.ServerState{
		InterfaceVersion: runtime.InterfaceVersion,
		PID:              pid,
		Address:          "http://" + listener.Addr().String(),
		Token:            token,
		Database:         paths.Database,
		StartedAt:        time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := bootstrap.WriteServerState(paths.ServerStateFile(), state); err != nil {
		return errors.Join(err, listener.Close(), rt.Close(context.Background()))
	}
	logger.Info("service_started",
		"go_version", goruntime.Version(),
		"spatial_version", rt.SpatialVersion(),
		"listen_address", listener.Addr().String(),
	)

	var server *httpserver.Server
	var shutdownOnce sync.Once
	var shutdownMu sync.Mutex
	var shutdownErr error
	shutdown := func() {
		shutdownOnce.Do(func() {
			if server != nil {
				server.BeginShutdown()
			}
			closeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			requestsErr := rt.BeginShutdown(closeCtx)
			var serverErr error
			if server != nil {
				serverErr = server.Shutdown(closeCtx)
			}
			runtimeErr := rt.Close(closeCtx)
			stateErr := bootstrap.RemoveServerState(paths.ServerStateFile(), pid, token)
			shutdownMu.Lock()
			shutdownErr = errors.Join(requestsErr, serverErr, runtimeErr, stateErr)
			shutdownMu.Unlock()
		})
	}
	server = httpserver.New(httpserver.Config{
		Runtime:        rt,
		Token:          token,
		ServiceVersion: runtime.ServiceVersion,
		DuckDBVersion:  runtime.DuckDBVersion,
		PID:            pid,
		Shutdown:       shutdown,
	})
	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	go func() {
		<-signalCtx.Done()
		shutdown()
	}()
	serveErr := server.Serve(listener)
	shutdown()
	shutdownMu.Lock()
	err = shutdownErr
	shutdownMu.Unlock()
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return errors.Join(serveErr, err)
	}
	return err
}

func runRestore(args []string) error {
	flags := newFlagSet("restore")
	database := flags.String("database", "", "persistent DuckDB database file")
	runtimeDir := flags.String("runtime-dir", "", "service runtime directory")
	backup := flags.String("backup", "", "verified complete backup directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	return restore.Restore(context.Background(), restore.Options{DatabasePath: *database, RuntimeDir: *runtimeDir, BackupPath: *backup})
}

func newFlagSet(name string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	return flags
}

func newToken() (string, error) {
	var data [32]byte
	if _, err := io.ReadFull(rand.Reader, data[:]); err != nil {
		return "", fmt.Errorf("generate server token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data[:]), nil
}

func removeStaleState(path string) error {
	state, err := bootstrap.ReadServerState(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read existing server state: %w", err)
	}
	request, err := http.NewRequest(http.MethodGet, strings.TrimRight(state.Address, "/")+"/health", nil)
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		request = request.WithContext(ctx)
		response, requestErr := http.DefaultClient.Do(request)
		cancel()
		if requestErr == nil {
			return errors.Join(errors.New("another geodata-serve is already running"), response.Body.Close())
		}
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale server state: %w", err)
	}
	return nil
}
