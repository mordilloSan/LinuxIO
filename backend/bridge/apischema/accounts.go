package apischema

type AccountUser struct {
	Gecos        string   `json:"gecos"`
	GID          int      `json:"gid"`
	Groups       []string `json:"groups"`
	HomeDir      string   `json:"homeDir"`
	IsLocked     bool     `json:"isLocked"`
	IsSystem     bool     `json:"isSystem"`
	LastLogin    string   `json:"lastLogin"`
	PrimaryGroup string   `json:"primaryGroup"`
	Shell        string   `json:"shell"`
	UID          int      `json:"uid"`
	Username     string   `json:"username"`
}

type AccountUserLogin struct {
	ID        string `json:"id"`
	Source    string `json:"source"`
	StartedAt string `json:"startedAt,omitempty"`
	Status    string `json:"status"`
	Terminal  string `json:"terminal"`
	Time      string `json:"time"`
	Username  string `json:"username"`
}

type AccountActiveSession struct {
	Idle      string `json:"idle,omitempty"`
	PID       *int   `json:"pid,omitzero"`
	SessionID string `json:"sessionId,omitempty"`
	Source    string `json:"source,omitempty"`
	StartedAt string `json:"startedAt"`
	Terminal  string `json:"terminal"`
}

type AccountPasswordState struct {
	Error         string `json:"error,omitempty"`
	Expires       string `json:"expires,omitempty"`
	ExpiresInDays *int   `json:"expiresInDays,omitzero"`
	HasPassword   bool   `json:"hasPassword"`
	LastChanged   string `json:"lastChanged,omitempty"`
	Locked        bool   `json:"locked"`
	MaxDays       *int   `json:"maxDays,omitzero"`
	WarningDays   *int   `json:"warningDays,omitzero"`
}

type AccountAdminAccess struct {
	Groups  []string `json:"groups"`
	IsAdmin bool     `json:"isAdmin"`
}

type AccountHomeHealth struct {
	Error        string `json:"error,omitempty"`
	Exists       bool   `json:"exists"`
	GroupGID     *int   `json:"groupGid,omitzero"`
	GroupName    string `json:"groupName,omitempty"`
	IsDirectory  bool   `json:"isDirectory"`
	Mode         string `json:"mode,omitempty"`
	OwnerMatches bool   `json:"ownerMatches"`
	OwnerUID     *int   `json:"ownerUid,omitzero"`
}

type AccountSSHAccess struct {
	AuthorizedKeysCount        int    `json:"authorizedKeysCount"`
	AuthorizedKeysExists       bool   `json:"authorizedKeysExists"`
	AuthorizedKeysMode         string `json:"authorizedKeysMode,omitempty"`
	AuthorizedKeysOwnerMatches bool   `json:"authorizedKeysOwnerMatches"`
	Error                      string `json:"error,omitempty"`
	SSHDirExists               bool   `json:"sshDirExists"`
	SSHDirMode                 string `json:"sshDirMode,omitempty"`
}

type AccountUserProcess struct {
	Command string  `json:"command"`
	CPU     float64 `json:"cpu"`
	Memory  float64 `json:"memory"`
	PID     int     `json:"pid"`
}

type AccountProcessSummary struct {
	Count int                  `json:"count"`
	Error string               `json:"error,omitempty"`
	Top   []AccountUserProcess `json:"top"`
}

type AccountUserDetails struct {
	ActiveSessions               []AccountActiveSession `json:"activeSessions"`
	Admin                        AccountAdminAccess     `json:"admin"`
	FailedLoginAttempts          int                    `json:"failedLoginAttempts"`
	FailedLoginAttemptsAvailable bool                   `json:"failedLoginAttemptsAvailable"`
	FailedLoginAttemptsError     string                 `json:"failedLoginAttemptsError,omitempty"`
	Home                         AccountHomeHealth      `json:"home"`
	Password                     AccountPasswordState   `json:"password"`
	Processes                    AccountProcessSummary  `json:"processes"`
	SSH                          AccountSSHAccess       `json:"ssh"`
	Username                     string                 `json:"username"`
}

type AccountGroup struct {
	GID      int      `json:"gid"`
	IsSystem bool     `json:"isSystem"`
	Members  []string `json:"members"`
	Name     string   `json:"name"`
}

type UsernameRequest struct {
	Username string `json:"username"`
}

type GroupNameRequest struct {
	GroupName string `json:"groupName"`
}

type ChangePasswordRequest struct {
	Username string `json:"username"`
	Password string `json:"password" secret:"true"`
}

type TerminateSessionRequest struct {
	SessionID string `json:"sessionId"`
	PID       string `json:"pid"`
}

type CreateUserRequest struct {
	Username   string   `json:"username"`
	Password   string   `json:"password" secret:"true"`
	FullName   *string  `json:"fullName,omitzero"`
	HomeDir    *string  `json:"homeDir,omitzero"`
	Shell      *string  `json:"shell,omitzero"`
	Groups     []string `json:"groups,omitempty"`
	CreateHome *bool    `json:"createHome,omitzero"`
}

type ModifyUserRequest struct {
	Username string   `json:"username"`
	FullName *string  `json:"fullName,omitzero"`
	HomeDir  *string  `json:"homeDir,omitzero"`
	Shell    *string  `json:"shell,omitzero"`
	Groups   []string `json:"groups,omitempty"`
}

type CreateGroupRequest struct {
	Name string `json:"name"`
	GID  *int   `json:"gid,omitzero"`
}

type ModifyGroupMembersRequest struct {
	GroupName string   `json:"groupName"`
	Members   []string `json:"members"`
}
