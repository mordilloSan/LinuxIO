package accounts

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

// These adapters are the only boundary between account collection structs and
// the public API contract. In particular, zero is a valid UID/GID, so the API
// keeps pointers there to distinguish a known root-owned path from unavailable
// ownership metadata.
func accountUserDetailsToAPI(value UserDetails) apischema.AccountUserDetails {
	return apischema.AccountUserDetails{
		Username:                     value.Username,
		ActiveSessions:               accountSessionsToAPI(value.ActiveSessions),
		FailedLoginAttempts:          value.FailedLoginAttempts,
		FailedLoginAttemptsAvailable: value.FailedLoginAttemptsAvailable,
		FailedLoginAttemptsError:     value.FailedLoginAttemptsError,
		Password: apischema.AccountPasswordState{
			Locked: value.Password.Locked, HasPassword: value.Password.HasPassword,
			LastChanged: value.Password.LastChanged, Expires: value.Password.Expires,
			ExpiresInDays: value.Password.ExpiresInDays, MaxDays: value.Password.MaxDays,
			WarningDays: value.Password.WarningDays, Error: value.Password.Error,
		},
		Admin: apischema.AccountAdminAccess{IsAdmin: value.Admin.IsAdmin, Groups: value.Admin.Groups},
		Home:  accountHomeToAPI(value.Home),
		SSH: apischema.AccountSSHAccess{
			SSHDirExists: value.SSH.SSHDirExists, AuthorizedKeysExists: value.SSH.AuthorizedKeysExists,
			AuthorizedKeysCount: value.SSH.AuthorizedKeysCount, SSHDirMode: value.SSH.SSHDirMode,
			AuthorizedKeysMode: value.SSH.AuthorizedKeysMode, AuthorizedKeysOwnerMatches: value.SSH.AuthorizedKeysOwnerMatches,
			Error: value.SSH.Error,
		},
		Processes: apischema.AccountProcessSummary{
			Count: value.Processes.Count, Error: value.Processes.Error, Top: accountProcessesToAPI(value.Processes.Top),
		},
	}
}

func accountHomeToAPI(value UserHomeHealth) apischema.AccountHomeHealth {
	result := apischema.AccountHomeHealth{
		Exists: value.Exists, IsDirectory: value.IsDirectory, GroupName: value.GroupName,
		OwnerMatches: value.OwnerMatches, Mode: value.Mode, Error: value.Error,
	}
	if value.ownershipKnown {
		result.OwnerUID = &value.OwnerUID
		result.GroupGID = &value.GroupGID
	}
	return result
}

func accountSessionsToAPI(values []UserActiveSession) []apischema.AccountActiveSession {
	result := make([]apischema.AccountActiveSession, len(values))
	for i, value := range values {
		result[i] = apischema.AccountActiveSession{
			Terminal: value.Terminal, StartedAt: value.StartedAt, Idle: value.Idle,
			SessionID: value.SessionID, Source: value.Source,
		}
		if value.PID > 0 {
			result[i].PID = &value.PID
		}
	}
	return result
}

func accountProcessesToAPI(values []UserProcess) []apischema.AccountUserProcess {
	result := make([]apischema.AccountUserProcess, len(values))
	for i, value := range values {
		result[i] = apischema.AccountUserProcess{PID: value.PID, Command: value.Command, CPU: value.CPU, Memory: value.Memory}
	}
	return result
}

func accountUserLoginsToAPI(values []UserLogin) []apischema.AccountUserLogin {
	result := make([]apischema.AccountUserLogin, len(values))
	for i, value := range values {
		result[i] = apischema.AccountUserLogin{
			ID: value.ID, Username: value.Username, Terminal: value.Terminal, Source: value.Source,
			Time: value.Time, StartedAt: value.StartedAt, Status: value.Status,
		}
	}
	return result
}

func accountGroupsToAPI(values []Group) []apischema.AccountGroup {
	result := make([]apischema.AccountGroup, len(values))
	for i, value := range values {
		result[i] = apischema.AccountGroup{Name: value.Name, GID: value.GID, Members: value.Members, IsSystem: value.IsSystem}
	}
	return result
}
