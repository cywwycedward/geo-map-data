package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"services/geodata-serve/internal/duckdbconn"
)

type ExtensionInfo struct {
	SpatialVersion string
}

func InstallExtensions(ctx context.Context, runtimeDir string) (info ExtensionInfo, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	layout, err := ResolveRuntimeLayout(runtimeDir)
	if err != nil {
		return info, fmt.Errorf("resolve runtime layout: %w", err)
	}
	if err := layout.EnsureDirectories(); err != nil {
		return info, err
	}
	database, err := duckdbconn.Open(ctx, duckdbconn.Config{
		DatabasePath:   ":memory:",
		ExtensionDir:   layout.ExtensionsDir,
		TempDir:        layout.DuckDBTempDir,
		MaxOpenConns:   1,
		LoadExtensions: false,
	})
	if err != nil {
		return info, fmt.Errorf("open extension installer: %w", err)
	}
	defer func() {
		err = errors.Join(err, database.Close())
	}()
	db := database.DB
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
