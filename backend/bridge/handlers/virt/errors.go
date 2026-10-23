package virt

import (
	"fmt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

func badRequestf(format string, args ...any) error {
	return bridgeipc.NewError(fmt.Sprintf(format, args...), 400)
}

func conflictf(format string, args ...any) error {
	return bridgeipc.NewError(fmt.Sprintf(format, args...), 409)
}

func notFoundf(format string, args ...any) error {
	return bridgeipc.NewError(fmt.Sprintf(format, args...), 404)
}
