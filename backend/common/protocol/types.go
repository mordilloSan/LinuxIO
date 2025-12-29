// Package protocol defines shared types and constants for LinuxIO auth communication.
// Keep in sync with packaging/linuxio_protocol.h
package protocol

// Max lengths for fields
const (
	MaxUsername   = 256
	MaxPassword   = 8192
	MaxSessionID  = 64
	MaxSecret     = 128
	MaxBridgePath = 4096
	MaxEnvMode    = 32
	MaxServerURL  = 512
	MaxServerCert = 16384
	MaxMotd       = 4096
	MaxError      = 256
)

// JSON field names - keep in sync with C header FIELD_* constants
const (
	FieldUser          = "user"
	FieldPassword      = "password"
	FieldSessionID     = "session_id"
	FieldBridgePath    = "bridge_path"
	FieldEnv           = "env"
	FieldVerbose       = "verbose"
	FieldSecret        = "secret"
	FieldServerBaseURL = "server_base_url"
	FieldServerCert    = "server_cert"
	FieldStatus        = "status"
	FieldError         = "error"
	FieldMode          = "mode"
	FieldMotd          = "motd"
	FieldUsername      = "username"
	FieldUID           = "uid"
	FieldGID           = "gid"
	FieldLogFD         = "log_fd"
)

// Status values
const (
	StatusOK    = "ok"
	StatusError = "error"
)

// Mode values
const (
	ModePrivileged   = "privileged"
	ModeUnprivileged = "unprivileged"
)

// Environment variable names
const (
	EnvSessionID  = "LINUXIO_SESSION_ID"
	EnvEnv        = "LINUXIO_ENV"
	EnvVerbose    = "LINUXIO_VERBOSE"
	EnvBridge     = "LINUXIO_BRIDGE"
	EnvPrivileged = "LINUXIO_PRIVILEGED"
)

// Environment mode values
const (
	EnvModeProduction  = "production"
	EnvModeDevelopment = "development"
)

// AuthRequest is the JSON request sent to the auth daemon (Server -> Auth)
type AuthRequest struct {
	User          string `json:"user"`
	Password      string `json:"password"`
	SessionID     string `json:"session_id"`
	BridgePath    string `json:"bridge_path,omitempty"`
	Env           string `json:"env,omitempty"`
	Verbose       string `json:"verbose,omitempty"`
	ServerBaseURL string `json:"server_base_url,omitempty"`
	ServerCert    string `json:"server_cert,omitempty"`
	Secret        string `json:"secret,omitempty"`
}

// AuthResponse is the JSON response from the auth daemon (Auth -> Server)
type AuthResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	Mode   string `json:"mode,omitempty"`
	Motd   string `json:"motd,omitempty"`
}

// Bootstrap is the configuration passed from auth daemon to bridge via stdin
type Bootstrap struct {
	SessionID     string `json:"session_id"`
	Username      string `json:"username"`
	UID           uint32 `json:"uid"`
	GID           uint32 `json:"gid"`
	Secret        string `json:"secret"`
	ServerBaseURL string `json:"server_base_url,omitempty"`
	ServerCert    string `json:"server_cert,omitempty"`
	Verbose       bool   `json:"verbose,omitempty"`
	LogFD         int    `json:"log_fd,omitempty"`
}

// IsPrivileged returns true if the mode indicates privileged access
func (r *AuthResponse) IsPrivileged() bool {
	return r.Mode == ModePrivileged
}

// IsOK returns true if the response status is OK
func (r *AuthResponse) IsOK() bool {
	return r.Status == StatusOK
}
