package system

import (
	"strconv"
	"strings"
)

func parseIncludeAllArg(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func parsePositiveLimitArg(args []string, fallback, max int) int {
	if fallback <= 0 {
		fallback = 24
	}
	if max <= 0 {
		max = fallback
	}
	if len(args) == 0 {
		return fallback
	}
	value, err := strconv.Atoi(strings.TrimSpace(args[0]))
	if err != nil || value <= 0 {
		return fallback
	}
	if value > max {
		return max
	}
	return value
}
