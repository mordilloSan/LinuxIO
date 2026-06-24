package virt

import (
	"errors"
	"fmt"

	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
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

func errorCode(err error, fallback int) int {
	var bridgeErr *bridgeipc.Error
	if errors.As(err, &bridgeErr) && bridgeErr.Code != 0 {
		return bridgeErr.Code
	}
	return fallback
}
