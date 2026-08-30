// Package duckdbconn owns the concrete DuckDB connection configuration and
// lifecycle used by the service and its short-lived verification databases.
package duckdbconn

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sync"

	"github.com/duckdb/duckdb-go/v2"
	"services/geodata-serve/internal/duckdbutil"
)

// Config describes one independently owned DuckDB pool.
type Config struct {
	DatabasePath   string
	ExtensionDir   string
	TempDir        string
	WorkingDir     string
	MaxOpenConns   int
	LoadExtensions bool
}

// Handle owns both the database pool and its connector. Callers must close
// the handle when the pool is no longer needed.
type Handle struct {
	DB        *sql.DB
	connector *duckdb.Connector
	closeOnce sync.Once
	closeErr  error
}

// Open creates a configured DuckDB pool and verifies that it can establish a
// connection. Every new underlying connection loads the service extensions
// and receives the explicit relative-file search roots.
func Open(ctx context.Context, config Config) (*Handle, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if config.DatabasePath == "" {
		return nil, errors.New("database path is required")
	}
	if config.DatabasePath != ":memory:" {
		path, err := absolutePath(config.DatabasePath)
		if err != nil {
			return nil, fmt.Errorf("database path: %w", err)
		}
		config.DatabasePath = path
	}
	for name, path := range map[string]*string{
		"extension": &config.ExtensionDir,
		"temp":      &config.TempDir,
		"working":   &config.WorkingDir,
	} {
		if *path == "" {
			continue
		}
		absolute, err := absolutePath(*path)
		if err != nil {
			return nil, fmt.Errorf("%s path: %w", name, err)
		}
		*path = absolute
	}
	values := url.Values{}
	if config.ExtensionDir != "" {
		values.Set("extension_directory", config.ExtensionDir)
	}
	if config.TempDir != "" {
		values.Set("temp_directory", config.TempDir)
	}
	dsn := config.DatabasePath
	if encoded := values.Encode(); encoded != "" {
		dsn += "?" + encoded
	}
	var callback func(driver.ExecerContext) error
	if config.LoadExtensions || config.WorkingDir != "" {
		callback = connectionInitializer(config.LoadExtensions, config.WorkingDir)
	}
	connector, err := duckdb.NewConnector(dsn, callback)
	if err != nil {
		return nil, fmt.Errorf("open DuckDB connector: %w", err)
	}
	handle := &Handle{DB: sql.OpenDB(connector), connector: connector}
	maxOpen := config.MaxOpenConns
	if maxOpen <= 0 {
		maxOpen = 1
	}
	handle.DB.SetMaxOpenConns(maxOpen)
	handle.DB.SetMaxIdleConns(maxOpen)
	if err := handle.DB.PingContext(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("open DuckDB database: %w", err), handle.Close())
	}
	return handle, nil
}

func connectionInitializer(loadExtensions bool, workingDir string) func(driver.ExecerContext) error {
	return func(execer driver.ExecerContext) error {
		if workingDir != "" {
			if _, err := execer.ExecContext(context.Background(), "SET file_search_path = "+duckdbutil.SQLLiteral(workingDir), nil); err != nil {
				return fmt.Errorf("set file search path: %w", err)
			}
			if _, err := execer.ExecContext(context.Background(), "SET home_directory = "+duckdbutil.SQLLiteral(workingDir), nil); err != nil {
				return fmt.Errorf("set home directory: %w", err)
			}
		}
		if loadExtensions {
			if err := loadExtensionsOnConnection(execer); err != nil {
				return err
			}
		}
		return nil
	}
}

func loadExtensionsOnConnection(execer driver.ExecerContext) error {
	if _, err := execer.ExecContext(context.Background(), "LOAD spatial", nil); err != nil {
		return fmt.Errorf("load spatial: %w", err)
	}
	if _, err := execer.ExecContext(context.Background(), "LOAD httpfs", nil); err != nil {
		return fmt.Errorf("load httpfs: %w", err)
	}
	return nil
}

// Close releases the pool and connector, preserving errors from both owners.
func (h *Handle) Close() error {
	if h == nil {
		return nil
	}
	h.closeOnce.Do(func() {
		h.closeErr = errors.Join(h.DB.Close(), h.connector.Close())
	})
	return h.closeErr
}

func absolutePath(path string) (string, error) {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}
