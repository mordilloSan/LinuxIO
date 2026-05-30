package system

import (
	"strconv"
	"strings"
)

func parsePositiveLimit(raw *string, fallback, max int) int {
	if fallback <= 0 {
		fallback = 24
	}
	if max <= 0 {
		max = fallback
	}
	if raw == nil {
		return fallback
	}
	value, err := strconv.Atoi(strings.TrimSpace(*raw))
	if err != nil || value <= 0 {
		return fallback
	}
	if value > max {
		return max
	}
	return value
}
