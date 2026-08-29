package runtime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/duckdb/duckdb-go/v2"
	"services/geodata-serve/internal/duckdbutil"
)

func (r *RuntimeModule) createBackup(ctx context.Context, requestID RequestID) error {
	if !validRequestID(requestID) {
		return errors.New("invalid request ID")
	}
	backupName := time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + string(requestID)
	backupPath := filepath.Join(r.backupDir, backupName)
	if !duckdbutil.PathWithin(r.backupDir, backupPath) {
		return errors.New("backup path escapes backup directory")
	}
	if err := os.Mkdir(backupPath, 0o700); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	keep := false
	defer func() {
		if !keep {
			if removeErr := os.RemoveAll(backupPath); removeErr != nil {
				r.logger.Error("backup_cleanup_failed", "error_code", "backup_failed", "error", removeErr)
			}
		}
	}()

	conn, err := r.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("get backup connection: %w", err)
	}
	_, execErr := conn.ExecContext(ctx, "EXPORT DATABASE "+duckdbutil.SQLLiteral(backupPath))
	closeErr := conn.Close()
	if execErr != nil {
		return errors.Join(backupError("export database", execErr), closeError(closeErr))
	}
	if closeErr != nil {
		return backupError("close backup connection", closeErr)
	}
	if err := verifyBackup(ctx, r.runtimeDir, r.extensionDir, backupPath); err != nil {
		return backupError("verify backup", err)
	}
	if err := os.WriteFile(filepath.Join(backupPath, duckdbutil.VerifiedBackupMarker), []byte("verified\n"), 0o600); err != nil {
		return backupError("write backup marker", err)
	}
	if err := r.cleanupBackups(); err != nil {
		return backupError("retain backups", err)
	}
	keep = true
	return nil
}

func verifyBackup(ctx context.Context, runtimeDir, extensionDir, backupPath string) (err error) {
	entries, err := os.ReadDir(backupPath)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return errors.New("export directory is empty")
	}
	verifyDir, err := os.MkdirTemp(filepath.Join(runtimeDir, "duckdb-tmp"), "verify-backup-")
	if err != nil {
		return err
	}
	defer func() {
		if removeErr := os.RemoveAll(verifyDir); removeErr != nil {
			err = errors.Join(err, fmt.Errorf("remove verification directory: %w", removeErr))
		}
	}()
	databasePath := filepath.Join(verifyDir, "verify.duckdb")
	dsn := databasePath + "?extension_directory=" + url.QueryEscape(extensionDir) + "&temp_directory=" + urlQuery(runtimeDir, "duckdb-tmp")
	connector, err := duckdb.NewConnector(dsn, duckdbutil.LoadExtensions)
	if err != nil {
		return fmt.Errorf("open verification connector: %w", err)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		return errors.Join(fmt.Errorf("open verification database: %w", err), db.Close(), connector.Close())
	}
	emptySchema, err := duckdbutil.EmptyExportSchema(backupPath)
	if err != nil {
		return errors.Join(fmt.Errorf("inspect exported schema: %w", err), db.Close(), connector.Close())
	}
	if !emptySchema {
		if _, err := db.ExecContext(ctx, "IMPORT DATABASE "+duckdbutil.SQLLiteral(backupPath)); err != nil {
			return errors.Join(fmt.Errorf("import exported database: %w", err), db.Close(), connector.Close())
		}
	}
	var one int
	queryErr := db.QueryRowContext(ctx, "SELECT 1").Scan(&one)
	if queryErr != nil {
		queryErr = fmt.Errorf("query imported database: %w", queryErr)
	}
	return errors.Join(queryErr, db.Close(), connector.Close())
}

func (r *RuntimeModule) cleanupBackups() error {
	entries, err := os.ReadDir(r.backupDir)
	if err != nil {
		return err
	}
	type backupEntry struct {
		path    string
		modTime time.Time
		name    string
	}
	backups := make([]backupEntry, 0)
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "." || entry.Name() == ".." || !validBackupName(entry.Name()) {
			continue
		}
		path := filepath.Join(r.backupDir, entry.Name())
		if !duckdbutil.PathWithin(r.backupDir, path) {
			return errors.New("backup cleanup path escapes backup directory")
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("refusing to clean symlink backup")
		}
		marker := filepath.Join(path, duckdbutil.VerifiedBackupMarker)
		if info, err := os.Lstat(marker); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		} else if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("backup verification marker is invalid")
		}
		markerData, err := os.ReadFile(marker)
		if err != nil {
			return err
		}
		if string(markerData) != "verified\n" {
			return errors.New("backup verification marker is invalid")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		backups = append(backups, backupEntry{path: path, modTime: info.ModTime(), name: entry.Name()})
	}
	sort.Slice(backups, func(i, j int) bool {
		if backups[i].modTime.Equal(backups[j].modTime) {
			return backups[i].name > backups[j].name
		}
		return backups[i].modTime.After(backups[j].modTime)
	})
	if len(backups) <= 5 {
		return nil
	}
	for _, backup := range backups[5:] {
		if !duckdbutil.PathWithin(r.backupDir, backup.path) || strings.EqualFold(filepath.Clean(backup.path), filepath.Clean(r.backupDir)) {
			return errors.New("refusing to remove backup outside backup directory")
		}
		if err := os.RemoveAll(backup.path); err != nil {
			return err
		}
	}
	return nil
}

func validRequestID(id RequestID) bool {
	if id == "" || !strings.HasPrefix(string(id), "req_") {
		return false
	}
	for _, char := range string(id) {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '_' && char != '-' {
			return false
		}
	}
	return true
}

func validBackupName(name string) bool {
	separator := strings.LastIndex(name, "-req_")
	if separator <= 0 {
		return false
	}
	if _, err := time.Parse("20060102T150405.000000000Z", name[:separator]); err != nil {
		return false
	}
	return validRequestID(RequestID(name[separator+1:]))
}

func closeError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("close backup connection: %w", err)
}

func backupError(stage string, err error) error {
	return errors.Join(ErrBackupFailed, fmt.Errorf("%s: %w", stage, err))
}

func urlQuery(runtimeDir, child string) string {
	return url.QueryEscape(filepath.Join(runtimeDir, child))
}
