package privilege

import (
	"context"
	"fmt"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// RequirePrivilegedIPC wraps an ipc.HandlerFunc to enforce privilege checking.
func RequirePrivilegedIPC(sess *session.Session, handler ipc.HandlerFunc) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if !sess.Privileged {
			return fmt.Errorf("operation requires administrator privileges")
		}
		return handler(ctx, args, emit)
	}
}
