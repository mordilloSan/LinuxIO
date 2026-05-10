package rpc

import (
	"encoding/json"

	"github.com/mordilloSan/LinuxIO/backend/bridge/privilege"
	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

type Command struct {
	Name       string
	Handler    ipc.HandlerFunc
	Privileged bool
}

func Register(component string, rt runtime.Runtime, commands []Command) {
	for _, cmd := range commands {
		handler := cmd.Handler
		if cmd.Privileged {
			handler = privilege.RequirePrivilegedIPC(rt.Session, handler)
		}
		ipc.RegisterFunc(component, cmd.Name, handler)
	}
}

func Arg(args []string, i int) (string, error) {
	if len(args) <= i {
		return "", ipc.ErrInvalidArgs
	}
	return args[i], nil
}

func RequireArgs(args []string, n int) error {
	if len(args) < n {
		return ipc.ErrInvalidArgs
	}
	return nil
}

func DecodeJSONArg[T any](args []string, i int) (T, error) {
	var zero T
	raw, err := Arg(args, i)
	if err != nil {
		return zero, err
	}
	var value T
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return zero, ipc.ErrInvalidArgs
	}
	return value, nil
}

func EmitResult(emit ipc.Events, result any, err error) error {
	if err != nil {
		return err
	}
	return emit.Result(result)
}
