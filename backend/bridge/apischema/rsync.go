package apischema

// RsyncConfig describes the single read-only backup module managed by LinuxIO.
type RsyncConfig struct {
	Module     string `json:"module"`
	Path       string `json:"path"`
	Username   string `json:"username"`
	NASAddress string `json:"nas_address"`
	Port       int    `json:"port"`
}

type RsyncSaveRequest struct {
	RsyncConfig
	// An empty password keeps the existing secret when the username is unchanged.
	Password string `json:"password"`
}

type RsyncStatus struct {
	Config  *RsyncConfig `json:"config,omitempty"`
	Active  bool         `json:"active"`
	Enabled bool         `json:"enabled"`
}

type RsyncSSHRequest struct {
	Port int `json:"port"`
}

// RsyncSSHConfig is the read-only module that `rsync --daemon` serves when
// TOS logs in over SSH as Username. It lives in that account's home directory.
type RsyncSSHConfig struct {
	Username string `json:"username"`
	Path     string `json:"path"`
	Module   string `json:"module"`
}

type RsyncSSHSaveRequest struct {
	Username string `json:"username"`
	Path     string `json:"path"`
}

// RsyncSSHStatus verifies the existing local SSH listener and reports the saved
// SSH module. SSH account authentication and file access are checked by the
// NAS's SSH connection.
type RsyncSSHStatus struct {
	Available bool            `json:"available"`
	Port      int             `json:"port"`
	Error     *string         `json:"error,omitempty"`
	Config    *RsyncSSHConfig `json:"config,omitempty"`
}
