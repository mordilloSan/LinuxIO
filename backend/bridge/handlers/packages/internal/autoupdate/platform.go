package autoupdate

import (
	"fmt"
	"strconv"
	"strings"
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
	values := make(map[string]string)
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[key] = strings.ToLower(strings.Trim(strings.TrimSpace(value), `"'`))
	}
	majorText, _, _ := strings.Cut(values["VERSION_ID"], ".")
	major, _ := strconv.Atoi(majorText)
	return hostPlatform{
		ID:           values["ID"],
		IDLike:       strings.Fields(values["ID_LIKE"]),
		VersionMajor: major,
	}
}
