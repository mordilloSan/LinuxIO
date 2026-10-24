package virt

import (
	"fmt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

func badRequestf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", bridgeipc.ErrInvalidArgs, fmt.Sprintf(format, args...))
}

func conflictf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", bridgeipc.ErrConflict, fmt.Sprintf(format, args...))
}

func notFoundf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", bridgeipc.ErrNotFound, fmt.Sprintf(format, args...))
}
