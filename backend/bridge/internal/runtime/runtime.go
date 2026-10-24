package runtime

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// Runtime carries process-wide bridge handler dependencies.
// Pass it by value; it is only two pointers.
type Runtime struct {
	Session *session.Session
	Store   *userconfig.UserStore
}

// New constructs a Runtime and panics if either dependency is nil.
func New(sess *session.Session, store *userconfig.UserStore) Runtime {
	if sess == nil {
		panic("runtime: nil session")
	}
	if store == nil {
		panic("runtime: nil store")
	}
	return Runtime{Session: sess, Store: store}
}

func (r Runtime) Username() string {
	return r.Session.User.Username
}
