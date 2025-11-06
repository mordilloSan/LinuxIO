package utils

import (
	"crypto/rand"
	math "math/rand"
	"strings"
	"time"
)

func GenerateKey() string {
	b := make([]byte, 64)
	_, err := rand.Read(b)
	if err != nil {
		return ""
	}
	return string(b)
}

func InsecureRandomIdentifier(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	math.New(math.NewSource(time.Now().UnixNano()))
	result := make([]byte, length)
	for i := range result {
		result[i] = charset[math.Intn(len(charset))]
	}
	return string(result)
}

func GetParentDirectoryPath(path string) string {
	if path == "/" || path == "" {
		return ""
	}
	path = strings.TrimSuffix(path, "/") // Remove trailing slash if any
	lastSlash := strings.LastIndex(path, "/")
	if lastSlash == -1 {
		return "" // No parent directory for a relative path without slashes
	}
	if lastSlash == 0 {
		return "/" // If the last slash is the first character, return root
	}
	return path[:lastSlash]
}
