package osuser

import (
	"os/user"
)

// LookupFunc is the function type for looking up OS users
type LookupFunc func(string) (*user.User, error)

// DefaultLookup is the default OS user lookup function
var DefaultLookup LookupFunc = user.Lookup

// Lookup wraps the OS user lookup to allow mocking in tests
func Lookup(username string) (*user.User, error) {
	return DefaultLookup(username)
}
