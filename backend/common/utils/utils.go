package utils

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mordilloSan/go_logger/v2/logger"
)

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
