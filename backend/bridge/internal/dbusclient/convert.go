package dbusclient

import "fmt"

func AsString(v any) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("expected string, got %T", v)
	}
	return s, nil
}

func AsUint32(v any) (uint32, error) {
	n, ok := v.(uint32)
	if !ok {
		return 0, fmt.Errorf("expected uint32, got %T", v)
	}
	return n, nil
}
