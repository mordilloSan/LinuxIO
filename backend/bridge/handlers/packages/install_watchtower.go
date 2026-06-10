package packages

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	tarpath "path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/system"
	bridgejobs "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

const (
	watchtowerReleaseBaseURL   = "https://github.com/nicholas-fedor/watchtower/releases/download"
	watchtowerArchiveMaxBytes  = 128 << 20
	watchtowerBinaryMaxBytes   = 128 << 20
	watchtowerChecksumMaxBytes = 64 << 10
	watchtowerHTTPTimeout      = 5 * time.Minute
)

type watchtowerAsset struct {
	name        string
	url         string
	checksumURL string
}

func installWatchtower(ctx context.Context, job *bridgejobs.Job) error {
	watchtowerVersion, err := configuredWatchtowerVersion()
	if err != nil {
		return err
	}

	asset, err := watchtowerAssetForArch(runtime.GOARCH, watchtowerVersion)
	if err != nil {
		return err
	}

	reportProgress(job, stageResolve, fmt.Sprintf("Resolving Watchtower %s", watchtowerVersion), pctResolve)
	slog.Info("Installing Watchtower.", "version", watchtowerVersion, "asset", asset.name)

	client := &http.Client{Timeout: watchtowerHTTPTimeout}

	expectedSHA256, err := downloadWatchtowerChecksum(ctx, client, asset)
	if err != nil {
		return fmt.Errorf("download Watchtower %s checksum: %w", watchtowerVersion, err)
	}

	reportProgress(job, stageInstallAsset, fmt.Sprintf("Downloading Watchtower %s", watchtowerVersion), pctInstallStart)
	archiveBytes, err := downloadWatchtowerAsset(ctx, client, asset)
	if err != nil {
		return fmt.Errorf("download Watchtower %s: %w", watchtowerVersion, err)
	}

	reportProgress(job, stageInstallAsset, fmt.Sprintf("Verifying Watchtower %s", watchtowerVersion), 70)
	if verifyErr := verifyWatchtowerAsset(asset, archiveBytes, expectedSHA256); verifyErr != nil {
		return verifyErr
	}

	reportProgress(job, stageInstallAsset, "Extracting Watchtower", 78)
	binaryBytes, err := extractWatchtowerBinary(archiveBytes)
	if err != nil {
		return fmt.Errorf("extract Watchtower binary: %w", err)
	}

	reportProgress(job, stageInstallAsset, fmt.Sprintf("Installing %s", system.WatchtowerBinaryName), 84)
	installPath := filepath.Join(version.BinDir, system.WatchtowerBinaryName)
	if err := utils.WriteFileAtomic(installPath, binaryBytes, 0o755); err != nil {
		return fmt.Errorf("install %s: %w", installPath, err)
	}

	reportProgress(job, stageInstallAsset, fmt.Sprintf("Installed Watchtower %s", watchtowerVersion), pctInstallEnd)
	slog.Info("Installed Watchtower.", "version", watchtowerVersion, "path", installPath)
	return nil
}

func configuredWatchtowerVersion() (string, error) {
	v := strings.TrimPrefix(strings.TrimSpace(version.WatchtowerVersion), "v")
	if v == "" {
		return "", fmt.Errorf("watchtower version is not configured")
	}
	return v, nil
}

func watchtowerAssetForArch(goarch, watchtowerVersion string) (watchtowerAsset, error) {
	assetArch, err := watchtowerAssetArch(goarch)
	if err != nil {
		return watchtowerAsset{}, err
	}

	name := fmt.Sprintf("watchtower_linux_%s_%s.tar.gz", assetArch, watchtowerVersion)
	tag := "v" + watchtowerVersion
	return watchtowerAsset{
		name:        name,
		url:         watchtowerReleaseBaseURL + "/" + tag + "/" + name,
		checksumURL: watchtowerReleaseBaseURL + "/" + tag + "/checksums.txt",
	}, nil
}

func watchtowerAssetArch(goarch string) (string, error) {
	switch goarch {
	case "amd64":
		return "amd64", nil
	case "arm64":
		return "arm64v8", nil
	case "arm":
		return "armhf", nil
	case "386":
		return "i386", nil
	case "riscv64":
		return "riscv64", nil
	default:
		return "", fmt.Errorf("unsupported Watchtower architecture %q", goarch)
	}
}

func downloadWatchtowerChecksum(ctx context.Context, client *http.Client, asset watchtowerAsset) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.checksumURL, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "text/plain")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http %d: %s", resp.StatusCode, readWatchtowerErrorBody(resp.Body))
	}
	body, err := utils.ReadAllLimited(resp.Body, watchtowerChecksumMaxBytes)
	if err != nil {
		return "", fmt.Errorf("read checksums: %w", err)
	}
	return parseWatchtowerChecksum(body, asset.name)
}

func parseWatchtowerChecksum(data []byte, assetName string) (string, error) {
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		candidate := strings.TrimPrefix(fields[1], "*")
		candidate = strings.TrimPrefix(candidate, "./")
		if candidate != assetName {
			continue
		}
		if !validSHA256Hex(fields[0]) {
			return "", fmt.Errorf("checksum for %s is not a valid SHA256 hex digest", assetName)
		}
		return strings.ToLower(fields[0]), nil
	}
	return "", fmt.Errorf("checksum for %s not found", assetName)
}

func validSHA256Hex(s string) bool {
	if len(s) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func downloadWatchtowerAsset(ctx context.Context, client *http.Client, asset watchtowerAsset) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, readWatchtowerErrorBody(resp.Body))
	}
	if resp.ContentLength > watchtowerArchiveMaxBytes {
		return nil, fmt.Errorf("archive exceeds %d bytes", watchtowerArchiveMaxBytes)
	}

	body, err := utils.ReadAllLimited(resp.Body, watchtowerArchiveMaxBytes)
	if err != nil {
		return nil, fmt.Errorf("read archive: %w", err)
	}
	return body, nil
}

func verifyWatchtowerAsset(asset watchtowerAsset, data []byte, expectedSHA256 string) error {
	hash := sha256.Sum256(data)
	actual := hex.EncodeToString(hash[:])
	if !strings.EqualFold(actual, expectedSHA256) {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", asset.name, expectedSHA256, actual)
	}
	return nil
}

func extractWatchtowerBinary(archiveBytes []byte) ([]byte, error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(archiveBytes))
	if err != nil {
		return nil, fmt.Errorf("open gzip: %w", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}
		if !header.FileInfo().Mode().IsRegular() {
			continue
		}
		if tarpath.Base(header.Name) != "watchtower" {
			continue
		}
		if header.Size <= 0 {
			return nil, fmt.Errorf("watchtower binary has invalid size %d", header.Size)
		}
		if header.Size > watchtowerBinaryMaxBytes {
			return nil, fmt.Errorf("watchtower binary exceeds %d bytes", watchtowerBinaryMaxBytes)
		}
		data, err := utils.ReadAllLimited(tarReader, watchtowerBinaryMaxBytes)
		if err != nil {
			return nil, fmt.Errorf("read binary: %w", err)
		}
		if int64(len(data)) != header.Size {
			return nil, fmt.Errorf("watchtower binary size mismatch: expected %d bytes, got %d", header.Size, len(data))
		}
		return data, nil
	}
	return nil, fmt.Errorf("watchtower binary not found in archive")
}

func readWatchtowerErrorBody(r io.Reader) string {
	body, err := utils.ReadAllLimited(r, 4<<10)
	if err != nil {
		return err.Error()
	}
	return string(body)
}
