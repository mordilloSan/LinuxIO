package ifconfig

import (
	"context"
	"os/exec"

	"github.com/mordilloSan/LinuxIO/backend/common/hostcmd"
)

type ExecRunner struct{}

func (ExecRunner) LookPath(name string) (string, error) {
	return hostcmd.LookPath(name)
}

func (r ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	path, err := r.LookPath(name)
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, path, args...).CombinedOutput()
}
