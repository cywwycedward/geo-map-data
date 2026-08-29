package contract

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"
)

const (
	ServiceVersion   = "0.1.0"
	InterfaceVersion = 1
	DuckDBVersion    = "1.4.5"
)

var (
	ErrShuttingDown   = errors.New("service is shutting down")
	ErrInvalidCommand = errors.New("invalid command")
	ErrBackupFailed   = errors.New("backup failed")
	ErrResultEncoding = errors.New("result encoding failed")
)

type Mode string

const (
	ModeRead  Mode = "read"
	ModeWrite Mode = "write"
)

type RequestID string

type Command struct {
	ID   RequestID
	Mode Mode
	SQL  string
}

type Column struct {
	Name       string
	DuckDBType string
}

type Event struct {
	Type        string
	RequestID   RequestID
	State       string
	At          time.Time
	Columns     []Column
	Values      []any
	RowCount    *int64
	QueuedMS    int64
	ExecutionMS int64
	Code        string
	Message     string
}

type EventSink func(Event) error

type RequestStatus struct {
	RequestID  RequestID
	Mode       Mode
	State      string
	AcceptedAt time.Time
	StartedAt  time.Time
	FinishedAt time.Time
	RowCount   *int64
	ErrorCode  string
}

type Runtime interface {
	Execute(context.Context, Command, EventSink) error
	Status(RequestID) (RequestStatus, bool)
	Close(context.Context) error
}

func NewRequestID() (RequestID, error) {
	var data [12]byte
	if _, err := io.ReadFull(rand.Reader, data[:]); err != nil {
		return "", fmt.Errorf("generate request ID: %w", err)
	}
	return RequestID("req_" + base64.RawURLEncoding.EncodeToString(data[:])), nil
}
