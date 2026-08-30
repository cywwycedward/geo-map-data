package restore

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"services/geodata-serve/internal/backup"
	"services/geodata-serve/internal/bootstrap"
	"services/geodata-serve/internal/duckdbutil"
)

type Options struct {
	DatabasePath string
	RuntimeDir   string
	BackupPath   string
}

func Restore(ctx context.Context, options Options) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if options.DatabasePath == "" || options.RuntimeDir == "" || options.BackupPath == "" {
		return errors.New("database, runtime, and backup paths are required")
	}
	databasePath, err := filepath.Abs(filepath.Clean(options.DatabasePath))
	if err != nil {
		return fmt.Errorf("database path: %w", err)
	}
	layout, err := bootstrap.ResolveRuntimeLayout(options.RuntimeDir)
	if err != nil {
		return err
	}
	backupPath, err := filepath.Abs(filepath.Clean(options.BackupPath))
	if err != nil {
		return fmt.Errorf("backup path: %w", err)
	}
	artifact, err := backup.Validate(backupPath)
	if err != nil {
		return err
	}
	if info, err := os.Lstat(databasePath); err == nil && info.IsDir() {
		return errors.New("database path is a directory")
	} else if err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("database path is a symlink")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("database path: %w", err)
	}
	if err := refuseRunningService(ctx, layout.ServerStateFile); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o700); err != nil {
		return fmt.Errorf("create database parent: %w", err)
	}
	if err := layout.EnsureDirectories(); err != nil {
		return err
	}
	store, err := backup.NewStore(backup.Config{
		BackupDir:    filepath.Dir(backupPath),
		ExtensionDir: layout.ExtensionsDir,
		TempDir:      layout.DuckDBTempDir,
	})
	if err != nil {
		return err
	}
	suffix, err := randomSuffix()
	if err != nil {
		return err
	}
	tempPath := filepath.Join(filepath.Dir(databasePath), "."+filepath.Base(databasePath)+".restore-"+suffix+".duckdb")
	if !duckdbutil.PathWithin(filepath.Dir(databasePath), tempPath) {
		return errors.New("restore temporary path escapes database directory")
	}
	defer func() {
		if removeErr := os.RemoveAll(tempPath); removeErr != nil {
			err = errors.Join(err, fmt.Errorf("remove restore temporary database: %w", removeErr))
		}
	}()
	if err := store.Import(ctx, artifact, tempPath); err != nil {
		return fmt.Errorf("import restore database: %w", err)
	}

	currentExists := false
	if info, err := os.Lstat(databasePath); err == nil {
		currentExists = !info.IsDir()
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect current database: %w", err)
	}
	preservedPath := ""
	if currentExists {
		preservedSuffix, err := randomSuffix()
		if err != nil {
			return err
		}
		preservedPath = databasePath + ".pre-restore-" + time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + preservedSuffix
		if err := os.Rename(databasePath, preservedPath); err != nil {
			return fmt.Errorf("preserve current database: %w", err)
		}
	}
	if err := os.Rename(tempPath, databasePath); err != nil {
		if currentExists {
			if rollbackErr := os.Rename(preservedPath, databasePath); rollbackErr != nil {
				return errors.Join(fmt.Errorf("replace database: %w", err), fmt.Errorf("restore preserved database: %w", rollbackErr))
			}
		}
		return fmt.Errorf("replace database: %w", err)
	}
	return nil
}

type serverState struct {
	Address string `json:"address"`
}

func refuseRunningService(ctx context.Context, statePath string) error {
	data, err := os.ReadFile(statePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read server state: %w", err)
	}
	var state serverState
	if err := json.Unmarshal(data, &state); err != nil {
		return errors.New("refusing restore because server state is invalid")
	}
	if state.Address == "" {
		return errors.New("refusing restore because server state has no address")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, strings.TrimRight(state.Address, "/")+"/health", nil)
	if err != nil {
		return errors.New("refusing restore because server address is invalid")
	}
	response, err := http.DefaultClient.Do(req)
	if err == nil {
		return errors.Join(errors.New("cannot restore while geodata-serve is running"), response.Body.Close())
	}
	return nil
}

func randomSuffix() (string, error) {
	var data [12]byte
	if _, err := io.ReadFull(rand.Reader, data[:]); err != nil {
		return "", fmt.Errorf("generate restore path: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data[:]), nil
}
