package virt

import (
	"regexp"
	"runtime"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

var validVMName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$`)

func validateVMName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return badRequestf("VM name is required")
	}
	if !validVMName.MatchString(name) || strings.Contains(name, "..") {
		return badRequestf("VM name must be 1-63 characters and contain only letters, numbers, dots, dashes, and underscores")
	}
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "-") {
		return badRequestf("VM name must not start with a dot or dash")
	}
	return nil
}

func validateCreateRequest(req apischema.VMCreateRequest) error {
	sourceType := normalizedVMSourceType(req.SourceType)
	if err := validateVMSourceType(req.SourceType); err != nil {
		return err
	}
	if err := validateVMName(req.Name); err != nil {
		return err
	}
	if err := validateVMResources(req.VCPUs, req.MemoryMB); err != nil {
		return err
	}
	if req.DiskGB <= 0 {
		return badRequestf("diskGB must be greater than 0")
	}
	if req.TemplateID != "" && (sourceType != vmSourceTypeImagePreset || !validTemplateID.MatchString(req.TemplateID)) {
		return badRequestf("templateId must identify a saved image template")
	}
	if err := validateVMNetworkName(req.Network); err != nil {
		return err
	}
	if err := validateVMFirmware(req.Firmware, false); err != nil {
		return err
	}
	return validateCreateSource(req, sourceType)
}

func validateVMResources(vcpus, memoryMB int) error {
	if vcpus <= 0 {
		return badRequestf("vcpus must be greater than 0")
	}
	if vcpus > runtime.NumCPU() {
		return badRequestf("vcpus must not exceed the host's %d logical CPUs", runtime.NumCPU())
	}
	if memoryMB < 256 {
		return badRequestf("memoryMB must be at least 256")
	}
	return nil
}

func validateCreateSource(req apischema.VMCreateRequest, sourceType apischema.VMSourceType) error {
	switch sourceType {
	case vmSourceTypeISO:
		if req.ISOPath == "" {
			return badRequestf("isoPath is required")
		}
	case vmSourceTypeImport:
		if strings.TrimSpace(req.ImportPath) == "" {
			return badRequestf("importPath is required")
		}
	case vmSourceTypeImagePreset:
		preset, err := imagePreset(req.ImagePresetID)
		if err != nil {
			return err
		}
		if preset.MinDiskGB > 0 && req.DiskGB < preset.MinDiskGB {
			return badRequestf("%s requires diskGB to be at least %d", preset.Label, preset.MinDiskGB)
		}
		if preset.NeedsCloudInit {
			return validateCloudInitRequest(req, preset)
		}
	}
	return nil
}
