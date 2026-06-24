package virt

import (
	"errors"

	libvirt "github.com/digitalocean/go-libvirt"
)

func isLibvirtErrNo(err error, want libvirt.ErrorNumber) bool {
	if lvErr, ok := errors.AsType[libvirt.Error](err); ok {
		return lvErr.Code == uint32(want)
	}
	return false
}

func isDomainMissing(err error) bool {
	return isLibvirtErrNo(err, libvirt.ErrNoDomain)
}

func isStorageVolMissing(err error) bool {
	return isLibvirtErrNo(err, libvirt.ErrNoStorageVol)
}

func isStoragePoolMissing(err error) bool {
	return isLibvirtErrNo(err, libvirt.ErrNoStoragePool)
}

func isNetworkMissing(err error) bool {
	return isLibvirtErrNo(err, libvirt.ErrNoNetwork)
}
