package network

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrUnsupportedBackend = errors.New("unsupported network backend")

func OpenBackend(env Environment, iface string) (Backend, error) {
	if strings.TrimSpace(iface) == "" {
		return nil, fmt.Errorf("interface name is required")
	}

	if backend, err := detectNetplanBackend(env, iface); backend != nil || err != nil {
		return backend, err
	}
	if backend, err := detectNMConnectionBackend(env, iface); backend != nil || err != nil {
		return backend, err
	}
	if backend, err := detectNetworkdBackend(env, iface); backend != nil || err != nil {
		return backend, err
	}
	if backend, err := detectIfupdownBackend(env, iface); backend != nil || err != nil {
		return backend, err
	}
	if backend, err := detectIfcfgBackend(env, iface); backend != nil || err != nil {
		return backend, err
	}
	return nil, fmt.Errorf("%w for interface %s", ErrUnsupportedBackend, iface)
}

func ReadConfigBestEffort(env Environment, iface string) (InterfaceConfig, bool, error) {
	backend, err := OpenBackend(env, iface)
	if err != nil {
		if errors.Is(err, ErrUnsupportedBackend) {
			return InterfaceConfig{}, false, nil
		}
		return InterfaceConfig{}, false, err
	}
	config, err := backend.Read()
	if err != nil {
		return InterfaceConfig{}, false, err
	}
	return config, true, nil
}

func detectIfcfgBackend(env Environment, iface string) (Backend, error) {
	path := filepath.Join(env.IfcfgDir, "ifcfg-"+iface)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &ifcfgBackend{baseBackend: baseBackend{env: env, iface: iface, path: path}}, nil
}
