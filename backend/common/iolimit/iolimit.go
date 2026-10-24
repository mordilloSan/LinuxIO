// Package iolimit reads bounded payloads from untrusted readers.
package iolimit

import (
	"fmt"
	"io"
)

// ReadAll reads r to the end and fails once it exceeds limit bytes.
func ReadAll(r io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response body exceeds %d bytes", limit)
	}
	return body, nil
}
