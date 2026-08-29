package duckdbutil

import (
	"bytes"
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const VerifiedBackupMarker = ".geodata-serve-verified"

func LoadExtensions(execer driver.ExecerContext) error {
	if _, err := execer.ExecContext(context.Background(), "LOAD spatial", nil); err != nil {
		return fmt.Errorf("load spatial: %w", err)
	}
	if _, err := execer.ExecContext(context.Background(), "LOAD httpfs", nil); err != nil {
		return fmt.Errorf("load httpfs: %w", err)
	}
	return nil
}

func SQLLiteral(value string) string {
	return "'" + strings.ReplaceAll(filepath.ToSlash(value), "'", "''") + "'"
}

func EmptyExportSchema(backupPath string) (bool, error) {
	path := filepath.Join(backupPath, "schema.sql")
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("exported schema is invalid")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	return len(bytes.TrimSpace(data)) == 0, nil
}

func PathWithin(root, target string) bool {
	root, rootErr := filepath.Abs(filepath.Clean(root))
	target, targetErr := filepath.Abs(filepath.Clean(target))
	if rootErr != nil || targetErr != nil {
		return false
	}
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
