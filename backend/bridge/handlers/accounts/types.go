package accounts

// User represents a system user account
type User struct {
	Username     string   `json:"username"`
	UID          int      `json:"uid"`
	GID          int      `json:"gid"`
	Gecos        string   `json:"gecos"`
	HomeDir      string   `json:"homeDir"`
	Shell        string   `json:"shell"`
	PrimaryGroup string   `json:"primaryGroup"`
	Groups       []string `json:"groups"`
	IsSystem     bool     `json:"isSystem"`
	IsLocked     bool     `json:"isLocked"`
	LastLogin    string   `json:"lastLogin"`
}

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

// CreateUserRequest contains the fields for creating a new user
type CreateUserRequest struct {
	Username   string   `json:"username"`
	Password   string   `json:"password"`
	FullName   string   `json:"fullName"`
	HomeDir    string   `json:"homeDir"`
	Shell      string   `json:"shell"`
	Groups     []string `json:"groups"`
	CreateHome bool     `json:"createHome"`
}

// ModifyUserRequest contains the fields for modifying a user
type ModifyUserRequest struct {
	Username string   `json:"username"`
	FullName *string  `json:"fullName,omitempty"`
	HomeDir  *string  `json:"homeDir,omitempty"`
	Shell    *string  `json:"shell,omitempty"`
	Groups   []string `json:"groups,omitempty"`
}

// CreateGroupRequest contains the fields for creating a new group
type CreateGroupRequest struct {
	Name string `json:"name"`
	GID  *int   `json:"gid,omitempty"`
}

// ModifyGroupMembersRequest contains the fields for modifying group members
type ModifyGroupMembersRequest struct {
	GroupName string   `json:"groupName"`
	Members   []string `json:"members"`
}
