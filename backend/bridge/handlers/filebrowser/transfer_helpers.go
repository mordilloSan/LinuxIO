package filebrowser

import "log/slog"

func logWriteErr(action string, err error) {
	if err != nil {
		slog.Debug("failed to write filebrowser transfer frame", "action", action, "error", err)
	}
}
