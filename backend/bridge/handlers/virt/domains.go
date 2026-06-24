package virt

import (
	"context"
	"fmt"
	"strings"

	libvirt "github.com/digitalocean/go-libvirt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func ListVMs(ctx context.Context) ([]apischema.VirtualMachine, error) {
	var out []apischema.VirtualMachine
	err := withLibvirtConn(ctx, func(conn libvirtConn) error {
		flags := libvirt.ConnectListDomainsActive | libvirt.ConnectListDomainsInactive
		domains, _, err := conn.ConnectListAllDomains(1, flags)
		if err != nil {
			return err
		}
		out = make([]apischema.VirtualMachine, 0, len(domains))
		for _, domain := range domains {
			vm, err := virtualMachineFromDomain(conn, domain)
			if err != nil {
				return fmt.Errorf("read VM %s: %w", domain.Name, err)
			}
			out = append(out, vm)
		}
		return nil
	})
	if out == nil {
		out = []apischema.VirtualMachine{}
	}
	return out, err
}

func GetVM(ctx context.Context, name string) (apischema.VirtualMachine, error) {
	if err := validateVMName(name); err != nil {
		return apischema.VirtualMachine{}, err
	}
	var vm apischema.VirtualMachine
	err := withLibvirtConn(ctx, func(conn libvirtConn) error {
		domain, err := lookupDomain(conn, name)
		if err != nil {
			return err
		}
		vm, err = virtualMachineFromDomain(conn, domain)
		return err
	})
	return vm, err
}

func lookupDomain(conn libvirtConn, name string) (libvirt.Domain, error) {
	domain, err := conn.DomainLookupByName(name)
	switch {
	case err == nil:
		return domain, nil
	case isDomainMissing(err):
		return libvirt.Domain{}, notFoundf("VM %q not found", name)
	default:
		return libvirt.Domain{}, fmt.Errorf("look up VM %q: %w", name, err)
	}
}

func virtualMachineFromDomain(conn libvirtConn, domain libvirt.Domain) (apischema.VirtualMachine, error) {
	state, _, err := conn.DomainGetState(domain, 0)
	if err != nil {
		return apischema.VirtualMachine{}, err
	}
	autostartRaw, err := conn.DomainGetAutostart(domain)
	if err != nil {
		autostartRaw = 0
	}
	xmlDoc, err := conn.DomainGetXMLDesc(domain, 0)
	if err != nil {
		return apischema.VirtualMachine{}, err
	}
	vm, err := parseVirtualMachine(domain, xmlDoc, domainStateName(state), autostartRaw != 0)
	if err != nil {
		return apischema.VirtualMachine{}, err
	}
	enrichVMNICLeases(conn, &vm)
	if vm.VCPUs == 0 || vm.MemoryMB == 0 {
		_, maxMemKiB, memoryKiB, vcpus, _, infoErr := conn.DomainGetInfo(domain)
		if infoErr == nil {
			if vm.VCPUs == 0 {
				vm.VCPUs = int(vcpus)
			}
			if vm.MemoryMB == 0 {
				if memoryKiB > 0 {
					vm.MemoryMB = int(memoryKiB / 1024)
				} else {
					vm.MemoryMB = int(maxMemKiB / 1024)
				}
			}
		}
	}
	return vm, nil
}

func enrichVMNICLeases(conn libvirtConn, vm *apischema.VirtualMachine) {
	for idx := range vm.NICs {
		nic := &vm.NICs[idx]
		if nic.Network == "" || nic.MAC == "" {
			continue
		}
		network, lookupErr := conn.NetworkLookupByName(nic.Network)
		if lookupErr != nil {
			continue
		}
		leases, _, leaseErr := conn.NetworkGetDhcpLeases(network, libvirt.OptString{nic.MAC}, 1, 0)
		if leaseErr != nil {
			continue
		}
		nic.IPAddresses = leaseIPAddresses(leases, nic.MAC)
	}
}

func leaseIPAddresses(leases []libvirt.NetworkDhcpLease, mac string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(leases))
	for _, lease := range leases {
		if lease.Ipaddr == "" || !leaseMatchesMAC(lease.Mac, mac) {
			continue
		}
		if _, ok := seen[lease.Ipaddr]; ok {
			continue
		}
		seen[lease.Ipaddr] = struct{}{}
		out = append(out, lease.Ipaddr)
	}
	return out
}

func leaseMatchesMAC(leaseMAC libvirt.OptString, mac string) bool {
	if len(leaseMAC) == 0 {
		return true
	}
	return strings.EqualFold(leaseMAC[0], mac)
}

func domainStateName(state int32) string {
	switch libvirt.DomainState(state) {
	case libvirt.DomainNostate:
		return "none"
	case libvirt.DomainRunning:
		return "running"
	case libvirt.DomainBlocked:
		return "blocked"
	case libvirt.DomainPaused:
		return "paused"
	case libvirt.DomainShutdown:
		return "shutdown"
	case libvirt.DomainShutoff:
		return "shutoff"
	case libvirt.DomainCrashed:
		return "crashed"
	case libvirt.DomainPmsuspended:
		return "suspended"
	default:
		return "unknown"
	}
}
