package apischema

import (
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

type CapabilitiesResponse struct {
	session.CapabilitiesAvailable
	session.CapabilitiesError
}
