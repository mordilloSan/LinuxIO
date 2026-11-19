// Package dockerupdate is the Docker image-update engine: update checks, the
// standalone and Compose replacement flows, the scheduled runner and its
// systemd timer, and the files they persist. It is shared by the bridge (whose
// docker.update_container job calls UpdateContainerWait, or RebuildContainerWait
// for a Compose service built on the host) and the root-sandboxed
// linuxio-docker-update binary (which runs the scheduled pass), so it imports
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
	DockerUpdateCheckStateBuilt       DockerUpdateCheckState = "built"
	DockerUpdateCheckStateError       DockerUpdateCheckState = "error"
)

// DockerContainerAutoUpdateSettings is every saved scheduled-update option.
// It is embedded in the wire type below and in the on-disk document, so a new
// saved option is one field here.
type DockerContainerAutoUpdateSettings struct {
	Cleanup        bool                          `json:"cleanup"`
	ContainerNames []string                      `json:"container_names"`
	IncludeStopped bool                          `json:"include_stopped"`
	Mode           DockerContainerAutoUpdateMode `json:"mode"`
	ReviveStopped  bool                          `json:"revive_stopped"`
	Time           string                        `json:"time"`
	UpdateAll      bool                          `json:"update_all"`
	UpdateStopped  bool                          `json:"update_stopped"`
}

// DockerContainerAutoUpdateOptions adds Enabled, which reports whether the
// systemd timer is on and is never saved.
type DockerContainerAutoUpdateOptions struct {
	DockerContainerAutoUpdateSettings
	Enabled bool `json:"enabled"`
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

// DockerContainerUpdateRequest names the container one update replaces.
// Rebuild asks for the image of its Compose service to be built again, with
// fresh base images, instead of pulled (RebuildContainerWait).
type DockerContainerUpdateRequest struct {
	ContainerID string `json:"containerId"`
	Rebuild     bool   `json:"rebuild,omitzero"`
}

// Target makes the container the job's target, so a second update of the
// same container is refused while one runs.
func (r DockerContainerUpdateRequest) Target() []string { return []string{r.ContainerID} }

type DockerUpdateCheckResult struct {
	// Built counts the containers whose image a Compose service builds on
	// this host (DockerUpdateCheckStateBuilt).
	Built   int `json:"built"`
	Checked int `json:"checked"`
	Errors  int `json:"errors"`
	// Rebuilds counts, among Updates, the built containers a base image of
	// whose image has a newer version: a rebuild, not an update, takes them.
	Rebuilds    int `json:"rebuilds"`
	Uncheckable int `json:"uncheckable"`
	// Updates counts every container with something waiting, Rebuilds
	// included.
	Updates int `json:"updates"`
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
