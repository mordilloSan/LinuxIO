package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"go-backend/internal/logger"
	"os"
	"os/user"
	"strings"
)

// GetDistroID reads /etc/os-release and extracts ID_LIKE
func GetDistroID() (string, error) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		logger.Errorf("❌ Failed to read /etc/os-release: %v", err)
		return "", err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "ID_LIKE=") {
			idLike := strings.Trim(strings.TrimPrefix(line, "ID_LIKE="), "\"")
			logger.Debugf("✅ Detected distro ID_LIKE: %s", idLike)
			return idLike, nil
		}
	}

	logger.Warnf("⚠️ ID_LIKE not found in /etc/os-release")
	return "", fmt.Errorf("ID_LIKE not found")
}

// GetDevPort returns the development port from env or defaults to 3000
func GetDevPort() string {
	port := os.Getenv("VITE_DEV_PORT")
	if port == "" {
		port = "3000"
		logger.Warnf("⚠️ VITE_DEV_PORT not set, defaulting to 3000")
	} else {
		logger.Debugf("🔧 VITE_DEV_PORT detected: %s", port)
	}
	return port
}

func GenerateSecretKey(n int) string {
	bytes := make([]byte, n)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func IsNumeric(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// getUserHome returns the current user's home directory.
func GetUserHome() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return u.HomeDir, nil
}
