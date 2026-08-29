package bootstrap

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/duckdb/duckdb-go/v2"
)

type ExtensionInfo struct {
	SpatialVersion string
}

func InstallExtensions(ctx context.Context, runtimeDir string) (info ExtensionInfo, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	runtimeDir, err = filepath.Abs(filepath.Clean(runtimeDir))
	if err != nil {
		return info, err
	}
	extensionsDir := filepath.Join(runtimeDir, "extensions")
	tempDir := filepath.Join(runtimeDir, "duckdb-tmp")
	for _, directory := range []string{runtimeDir, extensionsDir, tempDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return info, fmt.Errorf("create extension directory: %w", err)
		}
	}
	dsn := ":memory:?" + url.Values{
		"extension_directory": []string{extensionsDir},
		"temp_directory":      []string{tempDir},
	}.Encode()
	connector, err := duckdb.NewConnector(dsn, nil)
	if err != nil {
		return info, fmt.Errorf("open extension installer: %w", err)
	}
	db := sql.OpenDB(connector)
	defer func() {
		err = errors.Join(err, db.Close(), connector.Close())
	}()
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		return info, fmt.Errorf("open extension installer: %w", err)
	}
	for _, extension := range []string{"spatial", "httpfs"} {
		if _, err := db.ExecContext(ctx, "INSTALL "+extension); err != nil {
			return info, fmt.Errorf("install %s extension: %w", extension, err)
		}
		if _, err := db.ExecContext(ctx, "LOAD "+extension); err != nil {
			return info, fmt.Errorf("load %s extension: %w", extension, err)
		}
	}
	if err := db.QueryRowContext(ctx, "SELECT extension_version FROM duckdb_extensions() WHERE extension_name = 'spatial'").Scan(&info.SpatialVersion); err != nil {
		return info, fmt.Errorf("read spatial extension version: %w", err)
	}
	if info.SpatialVersion == "" {
		return info, errors.New("spatial extension version is empty")
	}
	var drivers int
	if err := db.QueryRowContext(ctx, "SELECT count(DISTINCT short_name) FROM ST_Drivers() WHERE short_name IN ('GeoJSON', 'ESRI Shapefile')").Scan(&drivers); err != nil {
		return info, fmt.Errorf("verify spatial drivers: %w", err)
	}
	if drivers != 2 {
		return info, errors.New("spatial extension is missing GeoJSON or ESRI Shapefile driver")
	}
	return info, nil
}
