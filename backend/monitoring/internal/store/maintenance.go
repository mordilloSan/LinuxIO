package store

import (
	"context"
	"fmt"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/plugins"
)

// RunMaintenance removes full-resolution history after its retention window.
func (s *Store) RunMaintenance(ctx context.Context, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackTx(tx, "run maintenance")

	for _, plugin := range plugins.HistoryCapable() {
		table := pluginHistoryTable(plugin)
		if _, err = tx.ExecContext(ctx, fmt.Sprintf(`
			DELETE FROM %s
			WHERE resolution = ? AND captured_at < ?
		`, table), resolution1m, now.Add(-s.historyRetentionDuration()).UnixMilli()); err != nil {
			return err
		}
	}

	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}
