package runtime

import (
	"context"
	"errors"
)

func (r *RuntimeModule) createBackup(ctx context.Context, requestID RequestID) error {
	if _, err := r.backupStore.Create(ctx, r.db, string(requestID)); err != nil {
		return errors.Join(ErrBackupFailed, err)
	}
	return nil
}
