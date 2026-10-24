package autoupdate

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/common/osrelease"
)

type hostPlatform struct {
	ID           string
	IDLike       []string
	VersionMajor int
}

func readPlatform(readFile func(string) ([]byte, error), path string) (hostPlatform, error) {
	data, err := readFile(path)
	if err != nil {
		return hostPlatform{}, fmt.Errorf("read distribution information: %w", err)
	}
	return parsePlatform(data), nil
}

func parsePlatform(data []byte) hostPlatform {
	info := osrelease.Parse(data)
	majorText, _, _ := strings.Cut(info["VERSION_ID"], ".")
	major, _ := strconv.Atoi(majorText)
	return hostPlatform{ID: info.ID(), IDLike: info.IDLike(), VersionMajor: major}
}
