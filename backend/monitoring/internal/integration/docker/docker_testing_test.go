package docker

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// ansiEscapePattern matches ANSI escape sequences (colors, cursor movement, etc.)
// This includes CSI sequences like \x1b[...m and simple escapes like \x1b[K
var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b[@-Z\\-_]`)
var dockerContainerIDPattern = regexp.MustCompile(`^[a-fA-F0-9]{12,64}$`)

const (
	// Number of log lines to request when fetching container logs
	dockerLogsTail = 200
	// Maximum size of a single log frame (1MB) to prevent memory exhaustion
	// A single log line larger than 1MB is likely an error or misconfiguration
	maxLogFrameSize = 1024 * 1024
	// Maximum total log content size (5MB) to prevent memory exhaustion
	// This provides a reasonable limit for network transfer and browser rendering
	maxTotalLogSize = 5 * 1024 * 1024
)

func validateContainerID(containerID string) error {
	if !dockerContainerIDPattern.MatchString(containerID) {
		return fmt.Errorf("invalid container id")
	}
	return nil
}

func buildDockerContainerEndpoint(containerID, action string, query url.Values) (string, error) {
	if err := validateContainerID(containerID); err != nil {
		return "", err
	}
	u := &url.URL{
		Scheme: "http",
		Host:   "localhost",
		Path:   fmt.Sprintf("/containers/%s/%s", url.PathEscape(containerID), action),
	}
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}
	return u.String(), nil
}

// getContainerInfo fetches the inspection data for a container.
func (dm *Manager) getContainerInfo(ctx context.Context, containerID string) ([]byte, error) {
	endpoint, err := buildDockerContainerEndpoint(containerID, "json", nil)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := dm.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("container info request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	// Remove sensitive environment variables from Config.Env.
	var containerInfo map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&containerInfo); err != nil {
		return nil, err
	}
	if config, ok := containerInfo["Config"].(map[string]any); ok {
		delete(config, "Env")
	}

	return json.Marshal(containerInfo)
}

// getLogs fetches the logs for a container.
func (dm *Manager) getLogs(ctx context.Context, containerID string) (string, error) {
	query := url.Values{
		"stdout": []string{"1"},
		"stderr": []string{"1"},
		"tail":   []string{fmt.Sprintf("%d", dockerLogsTail)},
	}
	endpoint, err := buildDockerContainerEndpoint(containerID, "logs", query)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}

	resp, err := dm.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("logs request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var builder strings.Builder
	contentType := resp.Header.Get("Content-Type")
	multiplexed := strings.HasSuffix(contentType, "multiplexed-stream")
	logReader := io.Reader(resp.Body)
	if !multiplexed {
		// Podman may return multiplexed logs without Content-Type. Sniff the first frame header
		// with a small buffered reader only when the header check fails.
		bufferedReader := bufio.NewReaderSize(resp.Body, 8)
		multiplexed = detectDockerMultiplexedStream(bufferedReader)
		logReader = bufferedReader
	}
	if err := decodeDockerLogStream(logReader, &builder, multiplexed); err != nil {
		return "", err
	}

	// Strip ANSI escape sequences from logs for clean display in web UI.
	logs := builder.String()
	if strings.Contains(logs, "\x1b") {
		logs = ansiEscapePattern.ReplaceAllString(logs, "")
	}
	return logs, nil
}

func detectDockerMultiplexedStream(reader *bufio.Reader) bool {
	const headerSize = 8
	header, err := reader.Peek(headerSize)
	if err != nil {
		return false
	}
	if header[0] != 0x01 && header[0] != 0x02 {
		return false
	}
	// Docker's stream framing header reserves bytes 1-3 as zero.
	if header[1] != 0 || header[2] != 0 || header[3] != 0 {
		return false
	}
	frameLen := binary.BigEndian.Uint32(header[4:])
	return frameLen <= maxLogFrameSize
}

func decodeDockerLogStream(reader io.Reader, builder *strings.Builder, multiplexed bool) error {
	if !multiplexed {
		_, err := io.Copy(builder, io.LimitReader(reader, maxTotalLogSize))
		return err
	}
	const headerSize = 8
	var header [headerSize]byte
	totalBytesRead := 0

	for {
		if _, err := io.ReadFull(reader, header[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return err
		}

		frameLen := binary.BigEndian.Uint32(header[4:])
		if frameLen == 0 {
			continue
		}

		// Prevent memory exhaustion from excessively large frames.
		if frameLen > maxLogFrameSize {
			return fmt.Errorf("log frame size (%d) exceeds maximum (%d)", frameLen, maxLogFrameSize)
		}

		// Check if reading this frame would exceed total log size limit.
		if totalBytesRead+int(frameLen) > maxTotalLogSize {
			// Read and discard remaining data to avoid blocking.
			_, _ = io.CopyN(io.Discard, reader, int64(frameLen))
			slog.Debug("Truncating logs: limit reached", "read", totalBytesRead, "limit", maxTotalLogSize)
			return nil
		}

		n, err := io.CopyN(builder, reader, int64(frameLen))
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return err
		}
		totalBytesRead += int(n)
	}
}
