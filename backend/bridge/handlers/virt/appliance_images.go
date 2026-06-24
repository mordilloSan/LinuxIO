package virt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	libvirt "github.com/digitalocean/go-libvirt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

const (
	vmSourceTypeISO          apischema.VMSourceType    = "iso"
	vmSourceTypeImagePreset  apischema.VMSourceType    = "imagePreset"
	vmImagePresetHomeOS      apischema.VMImagePresetID = "home-assistant-os"
	vmImagePresetDebian      apischema.VMImagePresetID = "debian-server"
	vmImagePresetUbuntu      apischema.VMImagePresetID = "ubuntu-server"
	vmImagePresetFedoraCloud apischema.VMImagePresetID = "fedora-cloud"
)

type vmImagePreset struct {
	ID               apischema.VMImagePresetID
	Label            string
	ImageURL         string
	ImageName        string
	ImageCompression string
	ReleaseAPIURL    string
	AssetNamePattern *regexp.Regexp
	DownloadPrefix   string
	MinDiskGB        int
	RequiresUEFI     bool
	NeedsCloudInit   bool
	CloudInitGroups  []string
}

type vmImageAsset struct {
	Name string
	URL  string
}

var (
	vmImagePresets = map[apischema.VMImagePresetID]vmImagePreset{
		vmImagePresetHomeOS: {
			ID:               vmImagePresetHomeOS,
			Label:            "Home Assistant OS",
			ReleaseAPIURL:    "https://api.github.com/repos/home-assistant/operating-system/releases/latest",
			AssetNamePattern: regexp.MustCompile(`^haos_ova-[^/]+\.qcow2\.xz$`),
			DownloadPrefix:   "https://github.com/home-assistant/operating-system/releases/download/",
			ImageCompression: "xz",
			MinDiskGB:        32,
			RequiresUEFI:     true,
		},
		vmImagePresetDebian: {
			ID:              vmImagePresetDebian,
			Label:           "Debian Server",
			ImageURL:        "https://cloud.debian.org/images/cloud/trixie/latest/debian-13-genericcloud-amd64.qcow2",
			ImageName:       "debian-13-genericcloud-amd64.qcow2",
			DownloadPrefix:  "https://cloud.debian.org/images/cloud/",
			MinDiskGB:       12,
			NeedsCloudInit:  true,
			CloudInitGroups: []string{"sudo"},
		},
		vmImagePresetUbuntu: {
			ID:              vmImagePresetUbuntu,
			Label:           "Ubuntu Server LTS",
			ImageURL:        "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img",
			ImageName:       "noble-server-cloudimg-amd64.img",
			DownloadPrefix:  "https://cloud-images.ubuntu.com/",
			MinDiskGB:       12,
			NeedsCloudInit:  true,
			CloudInitGroups: []string{"sudo"},
		},
		vmImagePresetFedoraCloud: {
			ID:              vmImagePresetFedoraCloud,
			Label:           "Fedora Cloud",
			ImageURL:        "https://download.fedoraproject.org/pub/fedora/linux/releases/44/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2",
			ImageName:       "Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2",
			DownloadPrefix:  "https://download.fedoraproject.org/pub/fedora/linux/releases/",
			MinDiskGB:       12,
			NeedsCloudInit:  true,
			CloudInitGroups: []string{"wheel"},
		},
	}
	vmImageHTTPClient     = &http.Client{}
	importImagePresetDisk = importImagePresetDiskFromNetwork
	createCloudInitSeed   = createCloudInitSeedISO
	execLookPath          = exec.LookPath
	execCommand           = exec.CommandContext
	removeFile            = os.Remove
	renameFile            = os.Rename
)

func normalizedVMSourceType(sourceType apischema.VMSourceType) apischema.VMSourceType {
	if sourceType == "" {
		return vmSourceTypeISO
	}
	return sourceType
}

func validateVMSourceType(sourceType apischema.VMSourceType) error {
	switch normalizedVMSourceType(sourceType) {
	case vmSourceTypeISO, vmSourceTypeImagePreset:
		return nil
	default:
		return badRequestf("unsupported VM source type %q", sourceType)
	}
}

func imagePreset(id apischema.VMImagePresetID) (vmImagePreset, error) {
	preset, ok := vmImagePresets[id]
	if !ok {
		return vmImagePreset{}, badRequestf("unsupported VM image preset %q", id)
	}
	return preset, nil
}

func createManagedImageStorage(ctx context.Context, conn libvirtConn, pool libvirt.StoragePool, volumeName string, req apischema.VMCreateRequest, report vmCreateReporter) (createdVMStorage, error) {
	volume, volumePath, volumeErr := createManagedImageVolume(ctx, conn, pool, volumeName, req, report)
	if volumeErr != nil {
		return createdVMStorage{}, volumeErr
	}
	storage := createdVMStorage{
		Boot: createdVMVolume{
			Volume: volume,
			Name:   volumeName,
			Path:   volumePath,
			SizeGB: req.DiskGB,
		},
	}
	preset, presetErr := imagePreset(req.ImagePresetID)
	if presetErr != nil {
		deleteCreatedStorage(conn, storage)
		return createdVMStorage{}, presetErr
	}
	if !preset.NeedsCloudInit {
		return storage, nil
	}
	seedName := managedSeedVolumeName(req.Name)
	reportVMCreateProgress(report, "seed", "Creating cloud-init seed image", filepath.Join(managedCloudPath, seedName), nil)
	seedVolume, seedErr := createManagedCloudInitSeed(ctx, conn, pool, seedName, req, preset)
	if seedErr != nil {
		deleteCreatedStorage(conn, storage)
		return createdVMStorage{}, seedErr
	}
	storage.Seed = &seedVolume
	return storage, nil
}

func createManagedImageVolume(ctx context.Context, conn libvirtConn, pool libvirt.StoragePool, volumeName string, req apischema.VMCreateRequest, report vmCreateReporter) (libvirt.StorageVol, string, error) {
	if err := ensureManagedVolumeAbsent(conn, pool, volumeName); err != nil {
		return libvirt.StorageVol{}, "", err
	}
	preset, presetErr := imagePreset(req.ImagePresetID)
	if presetErr != nil {
		return libvirt.StorageVol{}, "", presetErr
	}
	volumePath := filepath.Join(managedCloudPath, volumeName)
	if _, statErr := os.Stat(volumePath); statErr == nil {
		return libvirt.StorageVol{}, "", conflictf("managed volume path %q already exists", volumePath)
	}
	if importErr := importImagePresetDisk(ctx, preset, volumePath, req.DiskGB, report); importErr != nil {
		return libvirt.StorageVol{}, "", importErr
	}
	reportVMCreateProgress(report, "storage", "Refreshing default storage pool", volumePath, nil)
	if refreshErr := conn.StoragePoolRefresh(pool, 0); refreshErr != nil {
		_ = removeFile(volumePath)
		return libvirt.StorageVol{}, "", fmt.Errorf("refresh default storage pool: %w", refreshErr)
	}
	if volume, lookupErr := conn.StorageVolLookupByPath(volumePath); lookupErr == nil {
		return volume, volumePath, nil
	} else if !isStorageVolMissing(lookupErr) {
		return libvirt.StorageVol{}, "", fmt.Errorf("look up imported volume path: %w", lookupErr)
	}
	if volume, lookupErr := conn.StorageVolLookupByName(pool, volumeName); lookupErr == nil {
		return volume, volumePath, nil
	} else if !isStorageVolMissing(lookupErr) {
		return libvirt.StorageVol{}, "", fmt.Errorf("look up imported volume: %w", lookupErr)
	}
	return libvirt.StorageVol{Pool: defaultPoolName, Name: volumeName, Key: volumePath}, volumePath, nil
}

func importImagePresetDiskFromNetwork(ctx context.Context, preset vmImagePreset, volumePath string, diskGB int, report vmCreateReporter) error {
	reportVMCreateProgress(report, "resolve", "Resolving "+preset.Label+" image", "", nil)
	asset, assetErr := resolveImagePresetAsset(ctx, preset)
	if assetErr != nil {
		return fmt.Errorf("resolve %s image: %w", preset.Label, assetErr)
	}

	tmpXZ := volumePath + ".download"
	tmpDisk := volumePath + ".tmp"
	_ = removeFile(tmpXZ)
	_ = removeFile(tmpDisk)
	defer func() {
		_ = removeFile(tmpXZ)
		_ = removeFile(tmpDisk)
	}()

	if downloadErr := downloadImageAsset(ctx, preset, asset, tmpXZ, report); downloadErr != nil {
		return fmt.Errorf("download %s image: %w", preset.Label, downloadErr)
	}
	if assetCompressedWithXZ(preset, asset) {
		reportVMCreateProgress(report, "decompress", "Decompressing "+preset.Label+" image", tmpDisk, nil)
		if decompressErr := decompressXZFile(ctx, tmpXZ, tmpDisk); decompressErr != nil {
			return fmt.Errorf("decompress %s image: %w", preset.Label, decompressErr)
		}
	} else if renameErr := renameFile(tmpXZ, tmpDisk); renameErr != nil {
		return fmt.Errorf("stage %s image: %w", preset.Label, renameErr)
	}
	reportVMCreateProgress(report, "resize", fmt.Sprintf("Resizing %s disk to %d GB", preset.Label, diskGB), tmpDisk, nil)
	if resizeErr := resizeQCOW2Image(ctx, tmpDisk, diskGB); resizeErr != nil {
		return fmt.Errorf("resize %s image: %w", preset.Label, resizeErr)
	}
	reportVMCreateProgress(report, "finalize", "Finalizing "+preset.Label+" disk", volumePath, nil)
	if renameErr := renameFile(tmpDisk, volumePath); renameErr != nil {
		return fmt.Errorf("finalize %s image: %w", preset.Label, renameErr)
	}
	if accessErr := makeManagedWritableDiskAccessible(volumePath); accessErr != nil {
		return fmt.Errorf("prepare %s disk permissions: %w", preset.Label, accessErr)
	}
	return nil
}

func resolveImagePresetAsset(ctx context.Context, preset vmImagePreset) (vmImageAsset, error) {
	if preset.ImageURL != "" {
		if preset.DownloadPrefix != "" && !strings.HasPrefix(preset.ImageURL, preset.DownloadPrefix) {
			return vmImageAsset{}, fmt.Errorf("image URL for %s has unexpected download host", preset.Label)
		}
		name := preset.ImageName
		if name == "" {
			name = filepath.Base(preset.ImageURL)
		}
		return vmImageAsset{Name: name, URL: preset.ImageURL}, nil
	}
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, preset.ReleaseAPIURL, nil)
	if reqErr != nil {
		return vmImageAsset{}, reqErr
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "LinuxIO")
	resp, getErr := vmImageHTTPClient.Do(req)
	if getErr != nil {
		return vmImageAsset{}, getErr
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return vmImageAsset{}, fmt.Errorf("GitHub release API returned %s", resp.Status)
	}
	var release struct {
		Assets []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&release); decodeErr != nil {
		return vmImageAsset{}, decodeErr
	}
	for _, asset := range release.Assets {
		if !preset.AssetNamePattern.MatchString(asset.Name) {
			continue
		}
		if !strings.HasPrefix(asset.BrowserDownloadURL, preset.DownloadPrefix) {
			return vmImageAsset{}, fmt.Errorf("release asset %q has unexpected download host", asset.Name)
		}
		return vmImageAsset{Name: asset.Name, URL: asset.BrowserDownloadURL}, nil
	}
	return vmImageAsset{}, fmt.Errorf("no matching qcow2.xz release asset found")
}

func assetCompressedWithXZ(preset vmImagePreset, asset vmImageAsset) bool {
	return preset.ImageCompression == "xz" || strings.HasSuffix(asset.Name, ".xz")
}

func downloadImageAsset(ctx context.Context, preset vmImagePreset, asset vmImageAsset, destination string, report vmCreateReporter) error {
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if reqErr != nil {
		return reqErr
	}
	req.Header.Set("User-Agent", "LinuxIO")
	resp, getErr := vmImageHTTPClient.Do(req)
	if getErr != nil {
		return getErr
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download returned %s", resp.Status)
	}
	file, fileErr := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if fileErr != nil {
		return fileErr
	}
	defer file.Close()
	reportVMCreateProgress(report, "download", "Downloading "+preset.Label+" image", destination, progressPercent(0))
	reader := io.Reader(resp.Body)
	if report != nil {
		reader = newDownloadProgressReader(resp.Body, resp.ContentLength, func(downloaded, total int64, percent int) {
			message := fmt.Sprintf("Downloading %s image", preset.Label)
			if total > 0 {
				message = fmt.Sprintf("Downloading %s image (%s / %s)", preset.Label, formatBytes(downloaded), formatBytes(total))
			} else if downloaded > 0 {
				message = fmt.Sprintf("Downloading %s image (%s)", preset.Label, formatBytes(downloaded))
			}
			reportVMCreateProgress(report, "download", message, destination, progressPercent(percent))
		})
	}
	if _, copyErr := io.Copy(file, reader); copyErr != nil {
		return copyErr
	}
	return file.Close()
}

type downloadProgressReader struct {
	reader      io.Reader
	total       int64
	downloaded  int64
	lastPercent int
	report      func(downloaded, total int64, percent int)
}

func newDownloadProgressReader(reader io.Reader, total int64, report func(downloaded, total int64, percent int)) *downloadProgressReader {
	return &downloadProgressReader{
		reader:      reader,
		total:       total,
		lastPercent: -1,
		report:      report,
	}
}

func (r *downloadProgressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n <= 0 {
		return n, err
	}
	r.downloaded += int64(n)
	percent := 0
	if r.total > 0 {
		percent = int(float64(r.downloaded) / float64(r.total) * 100)
	}
	if r.total <= 0 || percent >= r.lastPercent+2 || percent == 100 {
		r.lastPercent = percent
		r.report(r.downloaded, r.total, percent)
	}
	return n, err
}

func formatBytes(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	value := float64(size)
	for _, suffix := range []string{"KiB", "MiB", "GiB", "TiB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f PiB", value/unit)
}

func decompressXZFile(ctx context.Context, source, destination string) error {
	xzPath, lookErr := execLookPath("xz")
	if lookErr != nil {
		return fmt.Errorf("xz is required to import compressed VM images")
	}
	out, fileErr := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if fileErr != nil {
		return fileErr
	}
	defer out.Close()

	var stderr bytes.Buffer
	cmd := execCommand(ctx, xzPath, "-dc", source)
	cmd.Stdout = out
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		return commandError(runErr, stderr.String())
	}
	return out.Close()
}

func resizeQCOW2Image(ctx context.Context, path string, diskGB int) error {
	qemuImgPath, lookErr := execLookPath("qemu-img")
	if lookErr != nil {
		return fmt.Errorf("qemu-img is required to resize imported VM images")
	}
	var stderr bytes.Buffer
	cmd := execCommand(ctx, qemuImgPath, "resize", "-f", "qcow2", path, fmt.Sprintf("%dG", diskGB))
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		return commandError(runErr, stderr.String())
	}
	return nil
}

func commandError(err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, stderr)
}
