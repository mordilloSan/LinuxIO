package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/user"
	"regexp"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/internal/logger"
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

// GetLocalIPByInterface returns the first IPv4 address found on the named interface.
// Returns "" if not found or on error.
func GetLocalIPByInterface(nicName string) (string, error) {
	iface, err := net.InterfaceByName(nicName)
	if err != nil {
		return "", fmt.Errorf("interface %q not found: %w", nicName, err)
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return "", fmt.Errorf("could not get addresses for %q: %w", nicName, err)
	}
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip != nil && ip.To4() != nil && !ip.IsLoopback() {
			return ip.String(), nil
		}
	}
	return "", fmt.Errorf("no IPv4 address found for interface %q", nicName)
}

func GetPublicIP() (string, error) {
	client := http.Client{
		Timeout: 5 * time.Second,
	}
	resp, err := client.Get("https://api.ipify.org")
	if err != nil {
		return "", err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			logger.Warnf("failed to close response body: %v", cerr)
		}
	}()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(body))
	return ip, nil
}

func StripANSI(input string) string {
	return regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`).ReplaceAllString(input, "")
}
