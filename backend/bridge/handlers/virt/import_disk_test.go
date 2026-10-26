package virt

import (
	"archive/tar"
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fakeImportCommands(t *testing.T, virtualSize string) {
	t.Helper()
	oldLook, oldCommand := execLookPath, execCommand
	execLookPath = func(name string) (string, error) { return name, nil }
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		switch args[0] {
		case "convert":
			return exec.CommandContext(ctx, "cp", args[5], args[6])
		case "info":
			return exec.CommandContext(ctx, "echo", `{"virtual-size": `+virtualSize+`}`)
		}
		return exec.CommandContext(ctx, "true")
	}
	t.Cleanup(func() { execLookPath, execCommand = oldLook, oldCommand })
}

func writeOVA(t *testing.T, ovf string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "appliance.ova")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := tar.NewWriter(file)
	for name, data := range map[string]string{"appliance.ovf": ovf, "../appliance-disk1.vmdk": "KDMV-bytes"} {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestImportDiskFileConvertsOVAAndDetectsBIOSFirmware(t *testing.T) {
	withFakeManagedStoragePermissions(t)
	fakeImportCommands(t, "10737418240")
	volumePath := filepath.Join(t.TempDir(), "linuxio-imported.qcow2")

	sizeGB, legacyBoot, err := importDiskFile(t.Context(), writeOVA(t, `<Envelope><vmw:Config vmw:key="firmware" vmw:value="bios"/></Envelope>`), volumePath, 20, nil)
	if err != nil {
		t.Fatalf("importDiskFile: %v", err)
	}
	if sizeGB != 20 || !legacyBoot {
		t.Fatalf("sizeGB=%d legacyBoot=%t, want 20 and BIOS boot", sizeGB, legacyBoot)
	}
	if data, readErr := os.ReadFile(volumePath); readErr != nil || string(data) != "KDMV-bytes" {
		t.Fatalf("volume = %q, %v", data, readErr)
	}
	if stage, _ := filepath.Glob(filepath.Join(filepath.Dir(volumePath), ".linuxio-create-*")); len(stage) != 0 {
		t.Fatalf("staging directory left behind: %v", stage)
	}
}

func TestImportDiskFileKeepsLargerImageAndEFIFirmware(t *testing.T) {
	withFakeManagedStoragePermissions(t)
	fakeImportCommands(t, "34359738368")
	volumePath := filepath.Join(t.TempDir(), "linuxio-imported.qcow2")

	sizeGB, legacyBoot, err := importDiskFile(t.Context(), writeOVA(t, `<Envelope><vmw:Config vmw:key="firmware" vmw:value="efi"/></Envelope>`), volumePath, 20, nil)
	if err != nil {
		t.Fatalf("importDiskFile: %v", err)
	}
	if sizeGB != 32 || legacyBoot {
		t.Fatalf("sizeGB=%d legacyBoot=%t, want 32 and UEFI boot", sizeGB, legacyBoot)
	}
}

func TestImportDiskFileDetectsTerraMasterOVMF(t *testing.T) {
	withFakeManagedStoragePermissions(t)
	fakeImportCommands(t, "34359738368")
	volumePath := filepath.Join(t.TempDir(), "linuxio-imported.qcow2")

	_, legacyBoot, err := importDiskFile(t.Context(), writeOVA(t, `<Envelope><VirtualSystem ovf:id="HomeAssistant" tnas:machine_type="q35" tnas:use_ovmf="true"/></Envelope>`), volumePath, 20, nil)
	if err != nil {
		t.Fatalf("importDiskFile: %v", err)
	}
	if legacyBoot {
		t.Fatal("legacyBoot = true, want UEFI boot for tnas:use_ovmf")
	}
}

func TestValidateImportDiskRejectsUnsupportedFile(t *testing.T) {
	if _, err := validateImportDisk(tempFile(t, "notes.txt")); err == nil || !strings.Contains(err.Error(), ".ova") {
		t.Fatalf("validateImportDisk error = %v, want unsupported extension rejection", err)
	}
	if _, err := validateImportDisk(t.TempDir()); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("validateImportDisk error = %v, want directory rejection", err)
	}
	path := tempFile(t, "disk.vmdk")
	if got, err := validateImportDisk(" " + path + " "); err != nil || got != path {
		t.Fatalf("validateImportDisk = %q, %v", got, err)
	}
}

func TestImportDiskFileRejectsVMDKDescriptor(t *testing.T) {
	fakeImportCommands(t, "10737418240")
	source := filepath.Join(t.TempDir(), "disk.vmdk")
	descriptor := "# Disk DescriptorFile\nversion=1\nRW 8 FLAT \"/etc/shadow\" 0\n"
	if err := os.WriteFile(source, []byte(descriptor), 0o600); err != nil {
		t.Fatal(err)
	}
	volumePath := filepath.Join(t.TempDir(), "linuxio-imported.qcow2")

	if _, _, err := importDiskFile(t.Context(), source, volumePath, 20, nil); err == nil || !strings.Contains(err.Error(), "sparse") {
		t.Fatalf("importDiskFile error = %v, want VMDK descriptor rejection", err)
	}
	if _, err := os.Stat(volumePath); err == nil {
		t.Fatal("volume was created from a VMDK descriptor")
	}
}

func TestCheckVMDKInfo(t *testing.T) {
	const path = "/stage/disk.vmdk"
	tests := []struct {
		name string
		info string
		want string
	}{
		{"own extent", `{"format-specific":{"data":{"extents":[{"filename":"` + path + `"}]}}}`, ""},
		{"no extents", `{"virtual-size":1}`, ""},
		{"other extent", `{"format-specific":{"data":{"extents":[{"filename":"` + path + `"},{"filename":"/etc/shadow"}]}}}`, "other files"},
		{"parent", `{"backing-filename":"/etc/shadow"}`, "parent"},
	}
	for _, tt := range tests {
		err := checkVMDKInfo([]byte(tt.info), path)
		if tt.want == "" && err != nil || tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)) {
			t.Errorf("%s: checkVMDKInfo error = %v, want %q", tt.name, err, tt.want)
		}
	}
}

func TestValidateVMDKDiskWithQEMUImg(t *testing.T) {
	qemuImg, err := exec.LookPath("qemu-img")
	if err != nil {
		t.Skip("qemu-img is not installed")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	// Sparse header (capacity 0) whose embedded descriptor names a FLAT extent.
	header := make([]byte, 512)
	copy(header, "KDMV")
	binary.LittleEndian.PutUint32(header[4:], 1)
	binary.LittleEndian.PutUint64(header[20:], 8)   // grainSize
	binary.LittleEndian.PutUint64(header[28:], 1)   // descriptorOffset
	binary.LittleEndian.PutUint64(header[36:], 20)  // descriptorSize
	binary.LittleEndian.PutUint32(header[44:], 512) // numGTEsPerGT
	descriptor := "# Disk DescriptorFile\nversion=1\nCID=fffffffe\nparentCID=ffffffff\ncreateType=\"monolithicFlat\"\n\nRW 8 FLAT \"target.txt\" 0\n"
	evil := filepath.Join(dir, "evil.vmdk")
	image := make([]byte, 21*512) // header sector plus the 20 descriptor sectors
	copy(image, header)
	copy(image[512:], descriptor)
	if err := os.WriteFile(evil, image, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateVMDKDisk(t.Context(), evil); err == nil || !strings.Contains(err.Error(), "other files") {
		t.Fatalf("validateVMDKDisk(embedded descriptor) error = %v, want other files rejection", err)
	}
	for _, subformat := range []string{"monolithicSparse", "streamOptimized"} {
		good := filepath.Join(dir, subformat+".vmdk")
		out, err := exec.CommandContext(t.Context(), qemuImg, "create", "-f", "vmdk", "-o", "subformat="+subformat, good, "1M").CombinedOutput()
		if err != nil {
			t.Fatalf("qemu-img create %s: %v: %s", subformat, err, out)
		}
		if err := validateVMDKDisk(t.Context(), good); err != nil {
			t.Fatalf("validateVMDKDisk(%s) error = %v", subformat, err)
		}
	}
}
