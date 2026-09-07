package store

import (
	"fmt"
	"time"
)

// RunMaintenance removes full-resolution history after its retention window.
func (s *Store) RunMaintenance(now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	for _, plugin := range historyCapablePluginNames() {
		table := pluginHistoryTable(plugin)
		if _, err = tx.Exec(fmt.Sprintf(`
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
