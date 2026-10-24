// Package dockerupdate is the Docker image-update engine: update checks, the
// standalone and Compose replacement flows, the scheduled runner and its
// systemd timer, the durable worker entry point and the files they persist.
// It is shared by the bridge (which launches and observes the worker) and the
// root-sandboxed linuxio-docker-update binary (which runs it), so it imports
// only common packages. The bridge re-exports the wire types below from
// bridge/apischema as aliases; the generated frontend contract names them by
// their Go type name, so renaming one is a contract change.
package dockerupdate

type DockerContainerAutoUpdateMode string
type DockerUpdateCheckState string

const (
	DockerUpdateCheckStateCurrent     DockerUpdateCheckState = "current"
	DockerUpdateCheckStateAvailable   DockerUpdateCheckState = "available"
	DockerUpdateCheckStateUncheckable DockerUpdateCheckState = "uncheckable"
	DockerUpdateCheckStateError       DockerUpdateCheckState = "error"
)

type DockerContainerAutoUpdateOptions struct {
	Cleanup        bool                          `json:"cleanup"`
	ContainerNames []string                      `json:"container_names"`
	Enabled        bool                          `json:"enabled"`
	IncludeStopped bool                          `json:"include_stopped"`
	Mode           DockerContainerAutoUpdateMode `json:"mode"`
	ReviveStopped  bool                          `json:"revive_stopped"`
	Time           string                        `json:"time"`
	UpdateAll      bool                          `json:"update_all"`
	UpdateStopped  bool                          `json:"update_stopped"`
}

type DockerContainerAutoUpdateTarget struct {
	ID              string  `json:"id"`
	Image           string  `json:"image"`
	Name            string  `json:"name"`
	Selected        bool    `json:"selected"`
	State           string  `json:"state"`
	MutationAllowed bool    `json:"mutationAllowed"`
	MutationReason  *string `json:"mutationReason,omitzero"`
}

type DockerContainerAutoUpdateState struct {
	Available             bool                              `json:"available"`
	Containers            []DockerContainerAutoUpdateTarget `json:"containers"`
	Error                 *string                           `json:"error,omitzero"`
	MissingContainerNames []string                          `json:"missing_container_names"`
	Options               DockerContainerAutoUpdateOptions  `json:"options"`
	TimerActive           bool                              `json:"timer_active"`
	TimerEnabled          bool                              `json:"timer_enabled"`
}

// DockerContainerUpdateRequest identifies one durable native Docker update.
// RunID is allocated by the client before the request so a retry after bridge
// loss can reclaim the same persisted operation instead of starting another
// mutation.
type DockerContainerUpdateRequest struct {
	ContainerID string `json:"containerId"`
	RunID       string `json:"runId"`
}

type DockerUpdateCheckResult struct {
	Checked     int `json:"checked"`
	Errors      int `json:"errors"`
	Uncheckable int `json:"uncheckable"`
	Updates     int `json:"updates"`
}

type DockerContainerUpdateResult struct {
	ContainerID     string `json:"containerId"`
	ContainerName   string `json:"containerName"`
	Error           string `json:"error,omitempty"`
	Image           string `json:"image"`
	NewImageID      string `json:"newImageId,omitempty"`
	PreviousImageID string `json:"previousImageId,omitempty"`
	Updated         bool   `json:"updated"`
}

// optional returns a pointer to value, or nil when it is the zero value so the
// field is omitted from the wire payload.
func optional[T comparable](value T) *T {
	var zero T
	if value == zero {
		return nil
	}
	return &value
}
