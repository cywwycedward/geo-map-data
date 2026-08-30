// Package backup owns the verified complete-backup artifact lifecycle.
package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"services/geodata-serve/internal/duckdbconn"
	"services/geodata-serve/internal/duckdbutil"
)

const (
	VerifiedMarker       = ".geodata-serve-verified"
	VerifiedBackupMarker = VerifiedMarker
	verifiedMarkerValue  = "verified\n"
	maxBackups           = 5
)

// Config contains the canonical directories used by backup verification and
// import. The backup directory is also the root under which online staging
// and published artifacts are created.
type Config struct {
	BackupDir    string
	ExtensionDir string
	TempDir      string
	WorkingDir   string
}

// Artifact is a backup directory whose structure and verification marker have
// passed this package's checks. The path cannot be constructed by callers
// without validation.
type Artifact struct {
	path string
}

func (a Artifact) Path() string { return a.path }

// Store owns creation, validation, import, and retention of backup artifacts.
type Store struct {
	config Config
}

func NewStore(config Config) (*Store, error) {
	if config.BackupDir == "" || config.ExtensionDir == "" || config.TempDir == "" {
		return nil, errors.New("backup, extension, and temporary directories are required")
	}
	backupDir, err := absolutePath(config.BackupDir)
	if err != nil {
		return nil, fmt.Errorf("backup directory: %w", err)
	}
	extensionDir, err := absolutePath(config.ExtensionDir)
	if err != nil {
		return nil, fmt.Errorf("extension directory: %w", err)
	}
	tempDir, err := absolutePath(config.TempDir)
	if err != nil {
		return nil, fmt.Errorf("temporary directory: %w", err)
	}
	workingDir := config.WorkingDir
	if workingDir != "" {
		workingDir, err = absolutePath(workingDir)
		if err != nil {
			return nil, fmt.Errorf("working directory: %w", err)
		}
	}
	if info, err := os.Lstat(backupDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("backup directory is a symlink")
		}
		if !info.IsDir() {
			return nil, errors.New("backup directory is not a directory")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("backup directory: %w", err)
	}
	return &Store{config: Config{BackupDir: backupDir, ExtensionDir: extensionDir, TempDir: tempDir, WorkingDir: workingDir}}, nil
}

// Create exports a complete database to a private staging directory, imports
// that export into a fresh database for verification, then publishes it with a
// complete marker before applying retention.
func (s *Store) Create(ctx context.Context, db *sql.DB, requestID string) (artifact Artifact, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if db == nil {
		return artifact, errors.New("database is required")
	}
	if !validRequestID(requestID) {
		return artifact, errors.New("invalid request ID")
	}
	stagingPath, err := os.MkdirTemp(s.config.BackupDir, ".geodata-serve-staging-")
	if err != nil {
		return artifact, fmt.Errorf("create backup staging directory: %w", err)
	}
	removeStaging := true
	defer func() {
		if removeStaging {
			if removeErr := os.RemoveAll(stagingPath); removeErr != nil {
				err = errors.Join(err, fmt.Errorf("remove backup staging directory: %w", removeErr))
			}
		}
	}()

	conn, err := db.Conn(ctx)
	if err != nil {
		return artifact, fmt.Errorf("get backup connection: %w", err)
	}
	_, exportErr := conn.ExecContext(ctx, "EXPORT DATABASE "+duckdbutil.SQLLiteral(stagingPath))
	closeErr := conn.Close()
	if exportErr != nil {
		return artifact, errors.Join(fmt.Errorf("export database: %w", exportErr), closeError(closeErr))
	}
	if closeErr != nil {
		return artifact, closeError(closeErr)
	}
	if err := ctx.Err(); err != nil {
		return artifact, err
	}
	if err := s.verifyExport(ctx, stagingPath); err != nil {
		return artifact, fmt.Errorf("verify backup: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return artifact, err
	}

	finalPath := filepath.Join(s.config.BackupDir, time.Now().UTC().Format("20060102T150405.000000000Z")+"-"+requestID)
	if !duckdbutil.PathWithin(s.config.BackupDir, finalPath) {
		return artifact, errors.New("backup path escapes backup directory")
	}
	if err := os.Rename(stagingPath, finalPath); err != nil {
		return artifact, fmt.Errorf("publish backup directory: %w", err)
	}
	stagingPath = finalPath
	if err := writeMarker(finalPath); err != nil {
		return artifact, fmt.Errorf("publish backup marker: %w", err)
	}
	artifact = Artifact{path: finalPath}
	if err := s.cleanup(); err != nil {
		return Artifact{}, fmt.Errorf("retain backups: %w", err)
	}
	removeStaging = false
	return artifact, nil
}

// Validate checks that path is a complete, service-verified artifact. It
// performs no DuckDB I/O; Import rechecks this immediately before opening a
// new database.
func Validate(path string) (Artifact, error) {
	path, err := absolutePath(path)
	if err != nil {
		return Artifact{}, fmt.Errorf("backup path: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return Artifact{}, fmt.Errorf("backup path: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Artifact{}, errors.New("backup path is not a directory")
	}
	if err := validateMarker(path); err != nil {
		return Artifact{}, err
	}
	if err := validateStructure(path); err != nil {
		return Artifact{}, err
	}
	return Artifact{path: path}, nil
}

func validateStructure(path string) error {
	schemaInfo, err := os.Lstat(filepath.Join(path, "schema.sql"))
	if err != nil {
		return errors.New("backup path is missing schema.sql")
	}
	if !schemaInfo.Mode().IsRegular() || schemaInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("backup schema is invalid")
	}
	return nil
}

// Import validates artifact again, imports it into databasePath, and verifies
// that the resulting database can execute a basic query. The target must be a
// new path selected by the restore owner.
func (s *Store) Import(ctx context.Context, artifact Artifact, databasePath string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if artifact.path == "" {
		return errors.New("backup artifact is required")
	}
	validated, err := Validate(artifact.path)
	if err != nil {
		return err
	}
	databasePath, err = absolutePath(databasePath)
	if err != nil {
		return fmt.Errorf("database path: %w", err)
	}
	if _, err := os.Lstat(databasePath); err == nil {
		return errors.New("restore database path already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect restore database path: %w", err)
	}
	return s.importDatabase(ctx, validated.path, databasePath)
}

func (s *Store) verifyExport(ctx context.Context, path string) (err error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return errors.New("export directory is empty")
	}
	if _, err := os.Lstat(filepath.Join(path, "schema.sql")); err != nil {
		return fmt.Errorf("exported schema: %w", err)
	}
	if err := validateStructure(path); err != nil {
		return err
	}
	verifyDir, err := os.MkdirTemp(s.config.TempDir, "verify-backup-")
	if err != nil {
		return err
	}
	defer func() {
		if removeErr := os.RemoveAll(verifyDir); removeErr != nil {
			err = errors.Join(err, fmt.Errorf("remove verification directory: %w", removeErr))
		}
	}()
	if err := s.importDatabase(ctx, path, filepath.Join(verifyDir, "verify.duckdb")); err != nil {
		return err
	}
	return nil
}

func (s *Store) importDatabase(ctx context.Context, backupPath, databasePath string) (err error) {
	handle, err := duckdbconn.Open(ctx, duckdbconn.Config{
		DatabasePath:   databasePath,
		ExtensionDir:   s.config.ExtensionDir,
		TempDir:        s.config.TempDir,
		WorkingDir:     s.config.WorkingDir,
		MaxOpenConns:   1,
		LoadExtensions: true,
	})
	if err != nil {
		return fmt.Errorf("open restore database: %w", err)
	}
	db := handle.DB
	defer func() { err = errors.Join(err, handle.Close()) }()
	emptySchema, err := duckdbutil.EmptyExportSchema(backupPath)
	if err != nil {
		return fmt.Errorf("inspect exported schema: %w", err)
	}
	if !emptySchema {
		// DuckDB emits an empty schema.sql for an empty database and rejects
		// IMPORT DATABASE with "empty query". Opening the fresh database and
		// running SELECT 1 below is the equivalent verification for that case.
		if _, err := db.ExecContext(ctx, "IMPORT DATABASE "+duckdbutil.SQLLiteral(backupPath)); err != nil {
			return fmt.Errorf("import database: %w", err)
		}
	}
	var one int
	if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
		return fmt.Errorf("verify imported database: %w", err)
	}
	return nil
}

func (s *Store) cleanup() error {
	rootInfo, err := os.Lstat(s.config.BackupDir)
	if err != nil {
		return err
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("backup directory is not a directory")
	}
	entries, err := os.ReadDir(s.config.BackupDir)
	if err != nil {
		return err
	}
	type entry struct {
		path    string
		name    string
		modTime time.Time
	}
	valid := make([]entry, 0)
	for _, item := range entries {
		if !item.IsDir() || !validBackupName(item.Name()) {
			continue
		}
		path := filepath.Join(s.config.BackupDir, item.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if err := validateMarker(path); err != nil {
			continue
		}
		if err := validateStructure(path); err != nil {
			continue
		}
		valid = append(valid, entry{path: path, name: item.Name(), modTime: info.ModTime()})
	}
	sort.Slice(valid, func(i, j int) bool {
		if valid[i].modTime.Equal(valid[j].modTime) {
			return valid[i].name > valid[j].name
		}
		return valid[i].modTime.After(valid[j].modTime)
	})
	if len(valid) <= maxBackups {
		return nil
	}
	for _, old := range valid[maxBackups:] {
		if !duckdbutil.PathWithin(s.config.BackupDir, old.path) || strings.EqualFold(filepath.Clean(old.path), filepath.Clean(s.config.BackupDir)) {
			return errors.New("refusing to remove backup outside backup directory")
		}
		if err := os.RemoveAll(old.path); err != nil {
			return err
		}
	}
	return nil
}

func validateMarker(path string) error {
	markerPath := filepath.Join(path, VerifiedMarker)
	info, err := os.Lstat(markerPath)
	if err != nil {
		return errors.New("backup path is not a verified geodata-serve backup")
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("backup verification marker is invalid")
	}
	marker, err := os.ReadFile(markerPath)
	if err != nil || string(marker) != verifiedMarkerValue {
		return errors.New("backup verification marker is invalid")
	}
	return nil
}

func writeMarker(path string) (err error) {
	marker, err := os.CreateTemp(path, ".geodata-serve-marker-")
	if err != nil {
		return err
	}
	tempPath := marker.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := marker.Chmod(0o600); err != nil {
		return err
	}
	if _, err := io.WriteString(marker, verifiedMarkerValue); err != nil {
		_ = marker.Close()
		return err
	}
	if err := marker.Sync(); err != nil {
		_ = marker.Close()
		return err
	}
	if err := marker.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, filepath.Join(path, VerifiedMarker)); err != nil {
		return err
	}
	removeTemp = false
	return nil
}

func validRequestID(id string) bool {
	if id == "" || !strings.HasPrefix(id, "req_") {
		return false
	}
	for _, char := range id {
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
	return validRequestID(name[separator+1:])
}

func closeError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("close backup connection: %w", err)
}

func absolutePath(path string) (string, error) {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}
