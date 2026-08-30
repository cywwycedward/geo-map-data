package bootstrap

import (
	"errors"
	"fmt"
	"os"
	"sync"
)

// EnterWorkingDirectory establishes the process working directory for a
// service lifetime. DuckDB's GDAL-backed spatial reader in the pinned v1.4.5
// build resolves ST_Read relative paths through the process CWD rather than
// file_search_path. Call this once before starting concurrent service work and
// invoke the returned restore function after the service stops.
func EnterWorkingDirectory(path string) (func() error, error) {
	path, err := absoluteClean(path)
	if err != nil {
		return nil, fmt.Errorf("working-dir path: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("working-dir: %w", err)
	}
	if !info.IsDir() {
		return nil, errors.New("working-dir is not a directory")
	}
	previous, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get current working directory: %w", err)
	}
	if err := os.Chdir(path); err != nil {
		return nil, fmt.Errorf("set working directory: %w", err)
	}
	var restoreOnce sync.Once
	var restoreErr error
	return func() error {
		restoreOnce.Do(func() { restoreErr = os.Chdir(previous) })
		return restoreErr
	}, nil
}
