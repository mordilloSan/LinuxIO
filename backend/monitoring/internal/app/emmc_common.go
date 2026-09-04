package app

import (
	"strconv"
	"strings"
)

func isEmmcBlockName(name string) bool {
	if !strings.HasPrefix(name, "mmcblk") {
		return false
	}
	suffix := strings.TrimPrefix(name, "mmcblk")
	if suffix == "" {
		return false
	}
	for _, c := range suffix {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func parseHexOrDecByte(s string) (uint8, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	base := 10
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		base = 16
		s = s[2:]
	}
	parsed, err := strconv.ParseUint(s, base, 8)
	if err != nil {
		return 0, false
	}
	return uint8(parsed), true
}

func parseHexBytePair(s string) (uint8, uint8, bool) {
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return 0, 0, false
	}
	a, okA := parseHexOrDecByte(fields[0])
	b, okB := parseHexOrDecByte(fields[1])
	if !okA && !okB {
		return 0, 0, false
	}
	return a, b, true
}

func emmcSmartStatus(preEOL uint8) string {
	switch preEOL {
	case 0x01:
		return "PASSED"
	case 0x02:
		return "WARNING"
	case 0x03:
		return "FAILED"
	default:
		return "UNKNOWN"
	}
}
