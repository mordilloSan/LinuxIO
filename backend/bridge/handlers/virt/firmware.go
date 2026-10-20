package virt

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	libvirt "github.com/digitalocean/go-libvirt"
	"libvirt.org/go/libvirtxml"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

const libvirtNVRAMDir = "/var/lib/libvirt/qemu/nvram/"

// SetVMFirmware redefines a shut-off VM to boot with UEFI or SeaBIOS.
func SetVMFirmware(ctx context.Context, req apischema.VMFirmwareRequest) (apischema.VirtualMachine, error) {
	if err := validateVMName(req.Name); err != nil {
		return apischema.VirtualMachine{}, err
	}
	if err := validateVMFirmware(req.Firmware, true); err != nil {
		return apischema.VirtualMachine{}, err
	}
	if req.Firmware == vmFirmwareUEFI && !ovmfAvailable() {
		return apischema.VirtualMachine{}, conflictf("OVMF firmware is not installed on this host")
	}
	var vm apischema.VirtualMachine
	err := withLibvirtConn(ctx, func(conn libvirtConn) error {
		domain, err := lookupDomain(conn, req.Name)
		if err != nil {
			return err
		}
		active, err := conn.DomainIsActive(domain)
		if err != nil {
			return fmt.Errorf("check VM active state: %w", err)
		}
		if active != 0 {
			return conflictf("VM %q must be shut off to change its firmware", req.Name)
		}
		domain, err = redefineDomainFirmware(conn, domain, req.Firmware)
		if err != nil {
			return err
		}
		vm, err = virtualMachineFromDomain(conn, domain)
		return err
	})
	return vm, err
}

func redefineDomainFirmware(conn libvirtConn, domain libvirt.Domain, firmware apischema.VMFirmware) (libvirt.Domain, error) {
	xmlDoc, err := conn.DomainGetXMLDesc(domain, libvirt.DomainXMLInactive)
	if err != nil {
		return domain, fmt.Errorf("read VM XML: %w", err)
	}
	var parsed libvirtxml.Domain
	if err = parsed.Unmarshal(xmlDoc); err != nil {
		return domain, fmt.Errorf("parse VM XML: %w", err)
	}
	if parsed.OS == nil {
		parsed.OS = &libvirtxml.DomainOS{}
	}
	if domainFirmware(parsed.OS) == firmware {
		return domain, nil
	}
	nvram := ""
	if parsed.OS.NVRam != nil {
		nvram = parsed.OS.NVRam.NVRam
	}
	setDomainFirmware(parsed.OS, firmware == vmFirmwareUEFI)
	updated, err := parsed.Marshal()
	if err != nil {
		return domain, err
	}
	redefined, err := conn.DomainDefineXML(updated)
	if err != nil {
		return domain, fmt.Errorf("redefine VM firmware: %w", err)
	}
	// libvirt only removes NVRAM on undefine; drop the stale vars file so a
	// later switch back to UEFI starts clean.
	if strings.HasPrefix(nvram, libvirtNVRAMDir) {
		if err = removeFile(nvram); err != nil && !errors.Is(err, os.ErrNotExist) {
			return redefined, fmt.Errorf("remove stale NVRAM %s: %w", nvram, err)
		}
	}
	return redefined, nil
}
