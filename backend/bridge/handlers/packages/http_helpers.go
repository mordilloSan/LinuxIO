package packages

import (
	"io"

	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

func readHTTPErrorBody(r io.Reader) string {
	body, err := utils.ReadAllLimited(r, 4<<10)
	if err != nil {
		return err.Error()
	}
	return string(body)
}
