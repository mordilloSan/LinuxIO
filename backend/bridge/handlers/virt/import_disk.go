package virt

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	libvirt "github.com/digitalocean/go-libvirt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// importDiskFormats maps accepted disk file extensions to the qemu-img input
// format, so QEMU never probes the format of a user-supplied file.
var importDiskFormats = map[string]string{
	".qcow2": "qcow2",
	".vmdk":  "vmdk",
	".vdi":   "vdi",
	".img":   "raw",
	".raw":   "raw",
}

// shortcut: a case-insensitive scan of the OVF for a firmware attribute set to
// EFI covers VMware (vmw:key="firmware" vmw:value="efi"), VirtualBox
// (<Firmware type="EFI"/>) and TerraMaster TOS (tnas:use_ovmf="true") exports;
// parse the OVF properly if other tools differ.
var ovfEFIFirmware = regexp.MustCompile(`(?i)firmware[^>]*efi|use_ovmf="true"`)

// ovfLegacyBoot reports whether an OVF descriptor asks for SeaBIOS. Without a
// descriptor the host default applies.
func ovfLegacyBoot(ovf []byte) bool {
	return len(ovf) > 0 && !ovfEFIFirmware.Match(ovf)
}

// readOVADescriptor returns the OVF inside an OVA without extracting its
// disks; tar skips entry bodies by seeking, so the scan stays cheap.
func readOVADescriptor(ovaPath string) ([]byte, error) {
	file, err := os.Open(ovaPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := tar.NewReader(file)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag == tar.TypeReg && strings.EqualFold(filepath.Ext(header.Name), ".ovf") {
			return io.ReadAll(io.LimitReader(reader, 8<<20))
		}
	}
}

func validateImportDisk(rawPath string) (string, error) {
	path, info, err := statHostFile(rawPath, "importPath", "import")
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", badRequestf("import path must point to a regular file: %s", path)
	}
	ext := strings.ToLower(filepath.Ext(path))
	if _, ok := importDiskFormats[ext]; !ok && ext != ".ova" {
		return "", badRequestf("import path must point to an .ova, .qcow2, .vmdk, .vdi, .img or .raw file: %s", path)
	}
	if !qemuReadable(path) {
		return "", badRequestf("import path is not readable by the bridge process: %s", path)
	}
	return path, nil
}

// validateVMDKDisk accepts only sparse-extent VMDKs (magic "KDMV", which OVA
// exports use). Text descriptors can name arbitrary host files as FLAT extents,
// and qemu-img would copy them into the converted disk.
func validateVMDKDisk(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	var magic [4]byte
	if _, readErr := io.ReadFull(file, magic[:]); readErr != nil || string(magic[:]) != "KDMV" {
		return badRequestf("VMDK must be a sparse or streamOptimized image; flat and split VMDK descriptors are not supported")
	}
	return nil
}

func createManagedImportStorage(ctx context.Context, conn libvirtConn, pool libvirt.StoragePool, volumeName string, req apischema.VMCreateRequest, report vmCreateReporter) (createdVMStorage, error) {
	var sizeGB int
	var legacyBoot bool
	volume, volumePath, err := publishManagedDiskVolume(conn, pool, volumeName, report, func(volumePath string) error {
		var importErr error
		sizeGB, legacyBoot, importErr = importDiskFile(ctx, req.ImportPath, volumePath, req.DiskGB, report)
		return importErr
	})
	if err != nil {
		return createdVMStorage{}, err
	}
	return createdVMStorage{
		Boot:       createdVMVolume{Volume: volume, Name: volumeName, Path: volumePath, SizeGB: sizeGB},
		LegacyBoot: legacyBoot,
	}, nil
}

// importDiskFile converts source (a disk image or an OVA) into a qcow2 at
// volumePath. minDiskGB only grows the disk; it never shrinks an image.
func importDiskFile(ctx context.Context, source, volumePath string, minDiskGB int, report vmCreateReporter) (int, bool, error) {
	stage, err := os.MkdirTemp(filepath.Dir(volumePath), ".linuxio-create-")
	if err != nil {
		return 0, false, fmt.Errorf("stage VM disk: %w", err)
	}
	defer os.RemoveAll(stage)

	legacyBoot := false
	if strings.EqualFold(filepath.Ext(source), ".ova") {
		reportVMCreateProgress(report, "extract", "Extracting OVA appliance", source, nil)
		var ovf []byte
		source, ovf, err = extractOVA(source, stage)
		if err != nil {
			return 0, false, fmt.Errorf("extract OVA: %w", err)
		}
		legacyBoot = ovfLegacyBoot(ovf)
	}
	format := importDiskFormats[strings.ToLower(filepath.Ext(source))]
	switch format {
	case "qcow2":
		if err = validateTemplateDisk(ctx, source); err != nil {
			return 0, false, fmt.Errorf("inspect imported image: %w", err)
		}
	case "vmdk":
		if err = validateVMDKDisk(source); err != nil {
			return 0, false, fmt.Errorf("inspect imported image: %w", err)
		}
	}
	tmpDisk := filepath.Join(stage, "disk.qcow2")
	reportVMCreateProgress(report, "convert", "Converting imported disk to qcow2", source, nil)
	if err = runTemplateQEMU(ctx, "convert", "-f", format, "-O", "qcow2", source, tmpDisk); err != nil {
		return 0, false, fmt.Errorf("convert imported disk: %w", err)
	}
	sizeGB, err := qcow2VirtualSizeGB(ctx, tmpDisk)
	if err != nil {
		return 0, false, fmt.Errorf("inspect converted disk: %w", err)
	}
	if minDiskGB > sizeGB {
		reportVMCreateProgress(report, "resize", fmt.Sprintf("Resizing imported disk to %d GB", minDiskGB), tmpDisk, nil)
		if err := resizeQCOW2Image(ctx, tmpDisk, minDiskGB); err != nil {
			return 0, false, fmt.Errorf("resize imported disk: %w", err)
		}
		sizeGB = minDiskGB
	}
	if err := makeManagedWritableDiskAccessible(tmpDisk); err != nil {
		return 0, false, fmt.Errorf("prepare imported disk permissions: %w", err)
	}
	reportVMCreateProgress(report, "finalize", "Finalizing imported disk", volumePath, nil)
	if err := os.Link(tmpDisk, volumePath); err != nil {
		return 0, false, fmt.Errorf("finalize imported disk: %w", err)
	}
	return sizeGB, legacyBoot, nil
}

// extractOVA writes the first disk image in the OVA into dir and returns its
// path with the OVF descriptor, if any. Entry names are flattened to their
// base name so the archive cannot write outside dir.
func extractOVA(ovaPath, dir string) (string, []byte, error) {
	file, err := os.Open(ovaPath)
	if err != nil {
		return "", nil, err
	}
	defer file.Close()
	reader := tar.NewReader(file)
	disk := ""
	var ovf []byte
	for disk == "" || ovf == nil {
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return "", nil, nextErr
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		name := filepath.Base(header.Name)
		if !filepath.IsLocal(name) {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		switch {
		case ext == ".ovf" && ovf == nil:
			ovf, err = io.ReadAll(io.LimitReader(reader, 8<<20))
		case importDiskFormats[ext] != "" && disk == "":
			disk = filepath.Join(dir, name)
			err = writeTarEntry(reader, disk)
		}
		if err != nil {
			return "", nil, err
		}
	}
	if disk == "" {
		return "", nil, badRequestf("OVA contains no supported disk image")
	}
	return disk, ovf, nil
}

func writeTarEntry(reader io.Reader, path string) error {
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, reader); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func qcow2VirtualSizeGB(ctx context.Context, path string) (int, error) {
	qemuImgPath, err := execLookPath("qemu-img")
	if err != nil {
		return 0, fmt.Errorf("qemu-img is required to inspect imported VM disks")
	}
	var stdout, stderr bytes.Buffer
	cmd := execCommand(ctx, qemuImgPath, "info", "--output=json", "-f", "qcow2", path)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return 0, commandError(err, stderr.String())
	}
	var info struct {
		VirtualSize int64 `json:"virtual-size"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		return 0, fmt.Errorf("parse qemu-img info: %w", err)
	}
	const gib = 1 << 30
	return int((info.VirtualSize + gib - 1) / gib), nil
}
