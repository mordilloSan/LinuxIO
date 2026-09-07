// Package utils provides utility functions for the agent.
package utils

import (
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
)

// GetEnv retrieves an environment variable by key.
func GetEnv(key string) (value string, exists bool) {
	return os.LookupEnv(key)
}

// BytesToMegabytes converts bytes to megabytes and rounds to two decimal places.
func BytesToMegabytes(b float64) float64 {
	return TwoDecimals(b / 1048576)
}

// BytesToGigabytes converts bytes to gigabytes and rounds to two decimal places.
func BytesToGigabytes(b uint64) float64 {
	return TwoDecimals(float64(b) / 1073741824)
}

// TwoDecimals rounds a float64 value to two decimal places.
func TwoDecimals(value float64) float64 {
	return math.Round(value*100) / 100
}

// ReadStringFile returns trimmed file contents or empty string on error.
func ReadStringFile(path string) string {
	content, _ := ReadStringFileOK(path)
	return content
}

// ReadStringFileOK returns trimmed file contents and read success.
func ReadStringFileOK(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

// ReadStringFileLimited reads a file into a string with a maximum size (in bytes) to avoid
// allocating large buffers and potential panics with pseudo-files when the size is misreported.
func ReadStringFileLimited(path string, maxSize int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	buf := make([]byte, maxSize)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return "", err
	}
	if n < 0 {
		return "", fmt.Errorf("%s returned negative bytes: %d", path, n)
	}
	return strings.TrimSpace(string(buf[:n])), nil
}

// FileExists reports whether the given path exists.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ReadUintFile parses a decimal uint64 value from a file.
func ReadUintFile(path string) (uint64, bool) {
	raw, ok := ReadStringFileOK(path)
	if !ok {
		return 0, false
	}
	parsed, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}
