package virt

import (
	"context"
	"fmt"
	"slices"
	"strings"

	libvirt "github.com/digitalocean/go-libvirt"
	"libvirt.org/go/libvirtxml"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// ListStoragePools returns every libvirt storage pool with its volumes.
func ListStoragePools(ctx context.Context) ([]apischema.VMStoragePool, error) {
	out := []apischema.VMStoragePool{}
	err := withLibvirtConn(ctx, func(conn libvirtConn) error {
		pools, _, err := conn.ConnectListAllStoragePools(1, 0)
		if err != nil {
			return fmt.Errorf("list storage pools: %w", err)
		}
		for _, pool := range pools {
			if err := ctx.Err(); err != nil {
				return err
			}
			item, err := storagePoolFromLibvirt(conn, pool)
			if err != nil {
				return fmt.Errorf("read storage pool %s: %w", pool.Name, err)
			}
			out = append(out, item)
		}
		return nil
	})
	slices.SortFunc(out, func(a, b apischema.VMStoragePool) int { return strings.Compare(a.Name, b.Name) })
	return out, err
}

func storagePoolFromLibvirt(conn libvirtConn, pool libvirt.StoragePool) (apischema.VMStoragePool, error) {
	out := apischema.VMStoragePool{Name: pool.Name, Volumes: []apischema.VMStorageVolume{}}
	xmlDoc, err := conn.StoragePoolGetXMLDesc(pool, 0)
	if err != nil {
		return out, err
	}
	var parsed libvirtxml.StoragePool
	if err = parsed.Unmarshal(xmlDoc); err != nil {
		return out, err
	}
	out.Type = parsed.Type
	if parsed.Target != nil {
		out.Path = parsed.Target.Path
	}
	active, err := conn.StoragePoolIsActive(pool)
	if err != nil {
		return out, err
	}
	out.Active = active != 0
	if !out.Active {
		return out, nil
	}
	_, out.CapacityBytes, out.AllocationBytes, out.AvailableBytes, err = conn.StoragePoolGetInfo(pool)
	if err != nil {
		return out, err
	}
	volumes, _, err := conn.StoragePoolListAllVolumes(pool, 1, 0)
	if err != nil {
		return out, err
	}
	for _, volume := range volumes {
		item := apischema.VMStorageVolume{Name: volume.Name}
		if _, item.CapacityBytes, item.AllocationBytes, err = conn.StorageVolGetInfo(volume); err != nil {
			return out, fmt.Errorf("volume %s: %w", volume.Name, err)
		}
		if item.Path, err = conn.StorageVolGetPath(volume); err != nil {
			return out, fmt.Errorf("volume %s: %w", volume.Name, err)
		}
		out.Volumes = append(out.Volumes, item)
	}
	slices.SortFunc(out.Volumes, func(a, b apischema.VMStorageVolume) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}
