package wireguard

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

func getPublicIP() (string, error) {
	client := http.Client{
		Timeout: 5 * time.Second,
	}
	resp, err := client.Get("https://api.ipify.org")
	if err != nil {
		return "", err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Warn("failed to close response body", "component", "wireguard", "error", cerr)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("public IP lookup returned %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

var getPublicIPFunc = getPublicIP

// resolveEndpointHost is what exported clients dial: the configured host
// override, else the detected public IP, else "" (the endpoint is omitted).
func resolveEndpointHost(cfg WireGuardConfig, logPrefix string) string {
	if cfg.Host != "" {
		return cfg.Host
	}
	publicIP, _ := getPublicIPFunc()
	if publicIP == "" {
		slog.Warn("public IP lookup returned empty string", "operation", logPrefix)
	}
	return publicIP
}
