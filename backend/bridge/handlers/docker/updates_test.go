package docker

import (
	"errors"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/dockerupdate"
)

// A scheduled-update setting the settings refuse is the caller's error: the
// bridge answers it as invalid arguments (400), before anything is written.
func TestSetContainerAutoUpdateAnswersInvalidSettingsAsInvalidArguments(t *testing.T) {
	for name, opts := range map[string]apischema.DockerContainerAutoUpdateOptions{
		"unknown mode":                {Mode: "daemon", Time: "04:00"},
		"time out of range":           {Mode: "update", Time: "25:00"},
		"negative free-space minimum": {Mode: "update", Time: "04:00", MinFreeGiB: -1},
		"revive without update":       {Mode: "update", Time: "04:00", ReviveStopped: true},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := SetContainerAutoUpdate(t.Context(), opts)
			if !errors.Is(err, dockerupdate.ErrInvalidSettings) || bridgeipc.StatusCode(err) != 400 {
				t.Fatalf("error = %v (status %d), want ErrInvalidSettings answered 400", err, bridgeipc.StatusCode(err))
			}
		})
	}
}
