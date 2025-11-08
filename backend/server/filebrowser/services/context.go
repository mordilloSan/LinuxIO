package services

// UserContext contains session user information and must be validated before file operations
type UserContext struct {
	Username    string // Session username (e.g., "miguelmariz")
	UID         string // Session UID as string (e.g., "1000")
	GID         string // Session GID as string (e.g., "1000")
	IsPrivileged bool  // Whether user has elevated privileges
}

// Validate checks if the UserContext is valid and coherent
func (uc *UserContext) Validate() error {
	if uc.Username == "" {
		return ErrMissingUsername
	}
	if uc.UID == "" {
		return ErrMissingUID
	}
	if uc.GID == "" {
		return ErrMissingGID
	}
	return nil
}

// Custom error types for user validation
var (
	ErrMissingUsername    = &userError{msg: "user context: missing username"}
	ErrMissingUID         = &userError{msg: "user context: missing UID"}
	ErrMissingGID         = &userError{msg: "user context: missing GID"}
	ErrPermissionDenied   = &userError{msg: "permission denied: insufficient privileges"}
	ErrUserMismatch       = &userError{msg: "user context: session user does not match requested operation"}
)

type userError struct {
	msg string
}

func (e *userError) Error() string {
	return e.msg
}

// NewUserError creates a new user validation error with context
func NewUserError(template string, args ...interface{}) error {
	return &userError{msg: "user context: " + (template)}
}
