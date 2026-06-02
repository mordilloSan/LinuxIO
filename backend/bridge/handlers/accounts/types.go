package accounts

// UserLogin represents one login event for a user account.
type UserLogin struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Terminal  string `json:"terminal"`
	Source    string `json:"source"`
	Time      string `json:"time"`
	StartedAt string `json:"startedAt,omitempty"`
	Status    string `json:"status"`
}

// UserActiveSession represents a currently active login session.
type UserActiveSession struct {
	Terminal  string `json:"terminal"`
	StartedAt string `json:"startedAt"`
	Idle      string `json:"idle,omitempty"`
	PID       int    `json:"pid,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
	Source    string `json:"source,omitempty"`
}

// UserPasswordState summarizes password aging and lock state.
type UserPasswordState struct {
	Locked        bool   `json:"locked"`
	HasPassword   bool   `json:"hasPassword"`
	LastChanged   string `json:"lastChanged,omitempty"`
	Expires       string `json:"expires,omitempty"`
	ExpiresInDays *int   `json:"expiresInDays,omitempty"`
	MaxDays       *int   `json:"maxDays,omitempty"`
	WarningDays   *int   `json:"warningDays,omitempty"`
	Error         string `json:"error,omitempty"`
}

// UserAdminAccess summarizes privilege-bearing group memberships.
type UserAdminAccess struct {
	IsAdmin bool     `json:"isAdmin"`
	Groups  []string `json:"groups"`
}

// UserHomeHealth summarizes ownership and permission health for the home path.
type UserHomeHealth struct {
	Exists       bool   `json:"exists"`
	IsDirectory  bool   `json:"isDirectory"`
	OwnerUID     int    `json:"ownerUid,omitempty"`
	GroupGID     int    `json:"groupGid,omitempty"`
	GroupName    string `json:"groupName,omitempty"`
	OwnerMatches bool   `json:"ownerMatches"`
	Mode         string `json:"mode,omitempty"`
	Error        string `json:"error,omitempty"`
}

// UserSSHAccess summarizes SSH authorized key availability.
type UserSSHAccess struct {
	SSHDirExists               bool   `json:"sshDirExists"`
	AuthorizedKeysExists       bool   `json:"authorizedKeysExists"`
	AuthorizedKeysCount        int    `json:"authorizedKeysCount"`
	SSHDirMode                 string `json:"sshDirMode,omitempty"`
	AuthorizedKeysMode         string `json:"authorizedKeysMode,omitempty"`
	AuthorizedKeysOwnerMatches bool   `json:"authorizedKeysOwnerMatches"`
	Error                      string `json:"error,omitempty"`
}

// UserProcess represents one process owned by the account.
type UserProcess struct {
	PID     int     `json:"pid"`
	Command string  `json:"command"`
	CPU     float64 `json:"cpu"`
	Memory  float64 `json:"memory"`
}

// UserProcessSummary summarizes processes currently owned by the account.
type UserProcessSummary struct {
	Count int           `json:"count"`
	Top   []UserProcess `json:"top"`
	Error string        `json:"error,omitempty"`
}

// UserDetails contains account health, security, and runtime detail.
type UserDetails struct {
	Username                     string              `json:"username"`
	ActiveSessions               []UserActiveSession `json:"activeSessions"`
	FailedLoginAttempts          int                 `json:"failedLoginAttempts"`
	FailedLoginAttemptsAvailable bool                `json:"failedLoginAttemptsAvailable"`
	FailedLoginAttemptsError     string              `json:"failedLoginAttemptsError,omitempty"`
	Password                     UserPasswordState   `json:"password"`
	Admin                        UserAdminAccess     `json:"admin"`
	Home                         UserHomeHealth      `json:"home"`
	SSH                          UserSSHAccess       `json:"ssh"`
	Processes                    UserProcessSummary  `json:"processes"`
}

// Group represents a system group
type Group struct {
	Name     string   `json:"name"`
	GID      int      `json:"gid"`
	Members  []string `json:"members"`
	IsSystem bool     `json:"isSystem"`
}

// UsernameRef identifies a user by username.
type UsernameRef struct {
	Username string `json:"username"`
}

// GroupNameRef identifies a group by name.
type GroupNameRef struct {
	GroupName string `json:"groupName"`
}
