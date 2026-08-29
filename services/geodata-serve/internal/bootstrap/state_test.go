package bootstrap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestServerStateIsWrittenAtomicallyAndRemovedOnlyForMatchingOwner(t *testing.T) {
	runtimeDir := t.TempDir()
	path := filepath.Join(runtimeDir, "server.json")
	want := ServerState{
		InterfaceVersion: 1,
		PID:              42,
		Address:          "http://127.0.0.1:12345",
		Token:            "secret-for-test",
		Database:         filepath.Join(runtimeDir, "db.duckdb"),
		StartedAt:        "2026-08-28T00:00:00Z",
	}

	if err := WriteServerState(path, want); err != nil {
		t.Fatalf("WriteServerState() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var got ServerState
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("server state is not JSON: %v", err)
	}
	if got != want {
		t.Fatalf("server state = %#v, want %#v", got, want)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("server state permissions are not private: %v", err)
		}
	}

	if err := RemoveServerState(path, 99, want.Token); err != nil {
		t.Fatalf("RemoveServerState(mismatched pid) error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("mismatched owner removed server state: %v", err)
	}
	if err := RemoveServerState(path, want.PID, want.Token); err != nil {
		t.Fatalf("RemoveServerState() error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("server state still exists after matching removal: %v", err)
	}
}
