package virt

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
)

var validTemplateID = regexp.MustCompile(`^[a-f0-9]{64}$`)
var templateStore = vmTemplateStore{path: managedRootPath + "/templates"}

type vmTemplateStore struct {
	path string
}

type vmTemplateRecord struct {
	apischema.VMTemplate
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"lastModified,omitempty"`
}

// Locks live outside version directories and are never removed. Each bridge
// session is a separate process; an in-memory mutex cannot protect this store.
func (s vmTemplateStore) withPresetLock(ctx context.Context, presetID apischema.VMImagePresetID, fn func() error) error {
	if _, err := imagePreset(presetID); err != nil {
		return err
	}
	return filelock.RunExclusive(ctx, filepath.Join(s.path, ".locks", string(presetID)), fn,
		filelock.WithDirPermissions(0o700))
}

func (s vmTemplateStore) list(ctx context.Context) (apischema.VMTemplateLibrary, error) {
	out := apischema.VMTemplateLibrary{Path: s.path, Templates: []apischema.VMTemplate{}}
	for id := range vmImagePresets {
		records, err := s.records(ctx, id)
		if err != nil {
			return out, err
		}
		for _, record := range records {
			out.Templates = append(out.Templates, record.VMTemplate)
		}
	}
	slices.SortFunc(out.Templates, func(a, b apischema.VMTemplate) int {
		if a.ImagePresetID != b.ImagePresetID {
			return strings.Compare(string(a.ImagePresetID), string(b.ImagePresetID))
		}
		return strings.Compare(b.DownloadedAt, a.DownloadedAt)
	})
	return out, nil
}

func (s vmTemplateStore) records(ctx context.Context, presetID apischema.VMImagePresetID) ([]vmTemplateRecord, error) {
	root, err := os.OpenRoot(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open template library: %w", err)
	}
	defer root.Close()
	dir, err := root.Open(string(presetID))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open template directory: %w", err)
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, fmt.Errorf("list template directory: %w", err)
	}
	var records []vmTemplateRecord
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() || !validTemplateID.MatchString(entry.Name()) {
			continue
		}
		record, readErr := s.readRecord(root, presetID, entry.Name())
		if errors.Is(readErr, os.ErrNotExist) {
			continue // Concurrent deletion.
		}
		if readErr != nil {
			return nil, readErr
		}
		records = append(records, record)
	}
	slices.SortFunc(records, func(a, b vmTemplateRecord) int {
		return strings.Compare(b.DownloadedAt, a.DownloadedAt)
	})
	return records, nil
}

func (s vmTemplateStore) readRecord(root *os.Root, presetID apischema.VMImagePresetID, id string) (vmTemplateRecord, error) {
	base := filepath.Join(string(presetID), id)
	data, err := root.ReadFile(filepath.Join(base, "metadata.json"))
	if err != nil {
		return vmTemplateRecord{}, fmt.Errorf("read template metadata: %w", err)
	}
	var record vmTemplateRecord
	if err = json.Unmarshal(data, &record); err != nil {
		return record, fmt.Errorf("decode template %s: %w", id, err)
	}
	imagePath := filepath.Join(base, "image.qcow2")
	info, err := root.Lstat(imagePath)
	if err != nil {
		return record, fmt.Errorf("inspect template image: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || record.ID != id || record.ImagePresetID != presetID {
		return record, fmt.Errorf("invalid saved template %s", id)
	}
	// Resolve placement from the store, never from persisted client data.
	record.Path = filepath.Join(s.path, imagePath)
	record.SizeBytes = info.Size()
	return record, nil
}

// Caller holds the preset lock through ensure and any copy from its result.
func (s vmTemplateStore) ensure(ctx context.Context, preset vmImagePreset, templateID string, refresh bool, report vmCreateReporter) (apischema.VMTemplate, error) {
	records, err := s.records(ctx, preset.ID)
	if err != nil {
		return apischema.VMTemplate{}, err
	}
	if templateID != "" {
		for _, record := range records {
			if record.ID == templateID {
				return record.VMTemplate, nil
			}
		}
		return apischema.VMTemplate{}, notFoundf("saved template %q is no longer available", templateID)
	}
	if !refresh && len(records) > 0 {
		return records[0].VMTemplate, nil
	}
	reportVMCreateProgress(report, "resolve", "Checking "+preset.Label+" template", "", nil)
	asset, err := resolveImagePresetAsset(ctx, preset)
	if err != nil {
		return apischema.VMTemplate{}, fmt.Errorf("resolve %s image: %w", preset.Label, err)
	}
	var previous *vmTemplateRecord
	for i := range records {
		if records[i].SourceURL == asset.URL {
			// Release assets have versioned URLs. Rolling distro URLs instead
			// use HTTP validators, preserving each downloaded snapshot.
			if preset.ReleaseAPIURL != "" || preset.ID == vmImagePresetFedoraCloud {
				return records[i].VMTemplate, nil
			}
			previous = &records[i]
			break
		}
	}
	return s.download(ctx, preset, asset, records, previous, report)
}

func (s vmTemplateStore) download(ctx context.Context, preset vmImagePreset, asset vmImageAsset, records []vmTemplateRecord, previous *vmTemplateRecord, report vmCreateReporter) (apischema.VMTemplate, error) {
	dir := filepath.Join(s.path, string(preset.ID))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return apischema.VMTemplate{}, fmt.Errorf("create template directory: %w", err)
	}
	// A fixed staging directory is safe under the process lock and lets the
	// next attempt discard partial files left by a terminated bridge.
	stage := filepath.Join(dir, ".download")
	if err := os.RemoveAll(stage); err != nil {
		return apischema.VMTemplate{}, err
	}
	if err := os.Mkdir(stage, 0o700); err != nil {
		return apischema.VMTemplate{}, err
	}
	defer os.RemoveAll(stage)
	source := filepath.Join(stage, "source")
	record, err := downloadImageAsset(ctx, preset, asset, source, previous, report)
	if err != nil {
		return apischema.VMTemplate{}, fmt.Errorf("download %s template: %w", preset.Label, err)
	}
	for _, saved := range records {
		if saved.ID == record.ID {
			return saved.VMTemplate, nil
		}
	}
	disk := filepath.Join(stage, "image.qcow2")
	if assetCompressedWithXZ(preset, asset) {
		reportVMCreateProgress(report, "decompress", "Decompressing "+preset.Label+" template", disk, nil)
		err = decompressXZFile(ctx, source, disk)
		if err == nil {
			err = os.Remove(source)
		}
	} else {
		err = os.Rename(source, disk)
	}
	if err != nil {
		return apischema.VMTemplate{}, fmt.Errorf("prepare template: %w", err)
	}
	if err = validateTemplateDisk(ctx, disk); err != nil {
		return apischema.VMTemplate{}, fmt.Errorf("validate template: %w", err)
	}
	info, err := os.Stat(disk)
	if err != nil {
		return apischema.VMTemplate{}, err
	}
	if err = os.Chmod(disk, 0o400); err != nil {
		return apischema.VMTemplate{}, err
	}
	version := asset.Version
	if version == "" {
		version = asset.Name
	}
	record.VMTemplate = apischema.VMTemplate{
		ID: record.ID, ImagePresetID: preset.ID, Label: preset.Label,
		Version: version, SourceURL: asset.URL, DownloadedAt: time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z"),
		SizeBytes: info.Size(), Path: filepath.Join(dir, record.ID, "image.qcow2"),
	}
	data, err := json.Marshal(record)
	if err != nil {
		return apischema.VMTemplate{}, err
	}
	if err := os.WriteFile(filepath.Join(stage, "metadata.json"), data, 0o600); err != nil {
		return apischema.VMTemplate{}, err
	}
	if err := ctx.Err(); err != nil {
		return apischema.VMTemplate{}, err
	}
	if err := os.Rename(stage, filepath.Join(dir, record.ID)); err != nil {
		return apischema.VMTemplate{}, fmt.Errorf("publish template: %w", err)
	}
	return record.VMTemplate, nil
}

func (s vmTemplateStore) delete(ctx context.Context, req apischema.VMTemplateRequest) error {
	if !validTemplateID.MatchString(req.TemplateID) {
		return badRequestf("invalid template ID")
	}
	return s.withPresetLock(ctx, req.ImagePresetID, func() error {
		root, err := os.OpenRoot(s.path)
		if err != nil {
			return err
		}
		defer root.Close()
		return root.RemoveAll(filepath.Join(string(req.ImagePresetID), req.TemplateID))
	})
}

func validateTemplateDisk(ctx context.Context, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	var header [104]byte
	if _, err := io.ReadFull(file, header[:72]); err != nil {
		return fmt.Errorf("read qcow2 header: %w", err)
	}
	version := binary.BigEndian.Uint32(header[4:8])
	if string(header[:4]) != "QFI\xfb" || (version != 2 && version != 3) {
		return fmt.Errorf("template is not a qcow2 image")
	}
	// Reject references to host files before allowing QEMU to open the image.
	// Header layout: https://www.qemu.org/docs/master/interop/qcow2.html
	if binary.BigEndian.Uint64(header[8:16]) != 0 {
		return fmt.Errorf("template must not reference a backing file")
	}
	if version == 3 {
		if _, err := io.ReadFull(file, header[72:]); err != nil {
			return fmt.Errorf("read qcow2 v3 header: %w", err)
		}
		if binary.BigEndian.Uint64(header[72:80])&(1<<2) != 0 {
			return fmt.Errorf("template must not reference an external data file")
		}
	}
	return runTemplateQEMU(ctx, "check", "-f", "qcow2", path)
}

func runTemplateQEMU(ctx context.Context, args ...string) error {
	path, err := execLookPath("qemu-img")
	if err != nil {
		return fmt.Errorf("qemu-img is required to prepare VM templates: %w", err)
	}
	var stderr bytes.Buffer
	cmd := execCommand(ctx, path, args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return commandError(err, stderr.String())
	}
	return nil
}
