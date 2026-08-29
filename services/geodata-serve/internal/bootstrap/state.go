package bootstrap

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ServerState is the process discovery record written while serve is running.
type ServerState struct {
	InterfaceVersion int    `json:"interface_version"`
	PID              int    `json:"pid"`
	Address          string `json:"address"`
	Token            string `json:"token"`
	Database         string `json:"database"`
	StartedAt        string `json:"started_at"`
}

func WriteServerState(path string, state ServerState) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".server.json-*")
	if err != nil {
		return fmt.Errorf("create temporary server state: %w", err)
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			if removeErr := os.Remove(tmpPath); removeErr != nil {
				err = errors.Join(err, fmt.Errorf("remove temporary server state: %w", removeErr))
			}
		}
	}()

	if err := tmp.Chmod(0o600); err != nil {
		return errors.Join(fmt.Errorf("protect temporary server state: %w", err), tmp.Close())
	}
	encoder := json.NewEncoder(tmp)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(state); err != nil {
		return errors.Join(fmt.Errorf("encode server state: %w", err), tmp.Close())
	}
	if err := tmp.Sync(); err != nil {
		return errors.Join(fmt.Errorf("sync server state: %w", err), tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary server state: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("publish server state: %w", err)
	}
	removeTemp = false
	return nil
}

func ReadServerState(path string) (ServerState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ServerState{}, err
	}
	var state ServerState
	if err := json.Unmarshal(data, &state); err != nil {
		return ServerState{}, fmt.Errorf("decode server state: %w", err)
	}
	return state, nil
}

// RemoveServerState removes a state file only when both owner fields match.
func RemoveServerState(path string, pid int, token string) error {
	state, err := ReadServerState(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if state.PID != pid || subtle.ConstantTimeCompare([]byte(state.Token), []byte(token)) != 1 {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove server state: %w", err)
	}
	return nil
}
