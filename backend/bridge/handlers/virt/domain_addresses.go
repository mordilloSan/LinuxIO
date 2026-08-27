package virt

import (
	"context"
	"time"

	libvirt "github.com/digitalocean/go-libvirt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

const domainAddressDiscoveryTimeout = 3 * time.Second

func enrichBridgeAddresses(ctx context.Context, conn libvirtConn, domains []libvirt.Domain, vms []apischema.VirtualMachine) {
	if !hasRunningBridgeNICs(vms) {
		return
	}
	discoveryCtx, cancel := context.WithTimeout(ctx, domainAddressDiscoveryTimeout)
	defer cancel()
	for idx := range domains {
		if idx >= len(vms) {
			break
		}
		if err := discoveryCtx.Err(); err != nil {
			return
		}
		enrichDomainBridgeAddresses(discoveryCtx, conn, domains[idx], &vms[idx])
	}
}

func hasRunningBridgeNICs(vms []apischema.VirtualMachine) bool {
	for idx := range vms {
		if vms[idx].State != "running" {
			continue
		}
		for _, nic := range vms[idx].NICs {
			if nic.AttachmentType == "bridge" && nic.MAC != "" {
				return true
			}
		}
	}
	return false
}

func enrichDomainBridgeAddresses(ctx context.Context, conn libvirtConn, domain libvirt.Domain, vm *apischema.VirtualMachine) {
	if vm.State != "running" {
		return
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return
	}
	arp, err := conn.DomainInterfaceAddresses(domain, uint32(libvirt.DomainInterfaceAddressesSrcArp), 0)
	if err != nil && ctx.Err() != nil {
		return
	}
	var unresolved []int
	for idx := range vm.NICs {
		nic := &vm.NICs[idx]
		if nic.AttachmentType != "bridge" || nic.MAC == "" {
			continue
		}
		nic.IPAddresses = interfaceIPAddresses(arp, nic.MAC)
		if len(nic.IPAddresses) == 0 {
			unresolved = append(unresolved, idx)
		}
	}
	if len(unresolved) == 0 {
		return
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return
	}
	agent, err := conn.DomainInterfaceAddresses(domain, uint32(libvirt.DomainInterfaceAddressesSrcAgent), 0)
	if err != nil {
		return
	}
	for _, idx := range unresolved {
		vm.NICs[idx].IPAddresses = interfaceIPAddresses(agent, vm.NICs[idx].MAC)
	}
}
