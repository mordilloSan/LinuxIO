package accounts

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

func createUserRequest(req apischema.CreateUserRequest) CreateUserRequest {
	out := CreateUserRequest{
		UsernameRef: UsernameRef{Username: req.Username},
		Password:    req.Password,
		UserProfileFields: UserProfileFields{
			Groups: req.Groups,
		},
	}
	if req.CreateHome != nil {
		out.CreateHome = *req.CreateHome
	}
	if req.FullName != nil {
		out.FullName = *req.FullName
	}
	if req.HomeDir != nil {
		out.HomeDir = *req.HomeDir
	}
	if req.Shell != nil {
		out.Shell = *req.Shell
	}
	return out
}

func modifyUserRequest(req apischema.ModifyUserRequest) ModifyUserRequest {
	return ModifyUserRequest{
		UsernameRef: UsernameRef{Username: req.Username},
		UserProfilePatch: UserProfilePatch{
			FullName: req.FullName,
			HomeDir:  req.HomeDir,
			Shell:    req.Shell,
			Groups:   req.Groups,
		},
	}
}
