package utils

import "strings"

func OptionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func StringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

//go:fix inline
func BoolPtr(value bool) *bool {
	return new(value)
}

func OptionalInt(value int) *int {
	if value == 0 {
		return nil
	}
	return &value
}

func OptionalUint64(value uint64) *uint64 {
	if value == 0 {
		return nil
	}
	return &value
}

func OptionalFloat64(value float64) *float64 {
	if value == 0 {
		return nil
	}
	return &value
}

func HasReadOnlyOpt(opts []string) bool {
	for _, o := range opts {
		if strings.TrimSpace(o) == "ro" {
			return true
		}
	}
	return false
}
