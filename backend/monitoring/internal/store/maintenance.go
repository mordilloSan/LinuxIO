package store

import (
	"context"
	"fmt"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/plugins"
)

// RunMaintenance deletes the rows each tier no longer keeps, in every history
// table, whether or not its plugin still records history.
func (s *Store) RunMaintenance(ctx context.Context, now time.Time) error {
	retention := s.historyRetentionDuration()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackTx(tx, "run maintenance")

	for _, plugin := range plugins.HistoryCapable() {
		table := pluginHistoryTable(plugin)
		for _, t := range tiers {
			if _, err = tx.ExecContext(ctx, fmt.Sprintf(`
				DELETE FROM %s
				WHERE resolution = ? AND captured_at < ?
			`, table), t.resolution, now.Add(-t.keepFor(retention)).UnixMilli()); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
