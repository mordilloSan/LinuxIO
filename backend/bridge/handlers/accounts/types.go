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
