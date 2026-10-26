package ifconfig

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

var ErrUnsupportedBackend = errors.New("unsupported network backend")

func OpenBackend(env Environment, iface string) (ConfigBackend, error) {
	if err := validateInterfaceName(iface); err != nil {
		return nil, err
	}

	if backend, err := DetectNetplanBackend(env, iface); backend != nil || err != nil {
		return backend, err
	}
	if backend, err := detectNMConnectionBackend(env, iface); backend != nil || err != nil {
		return backend, err
	}
	if backend, err := DetectNetworkdBackend(env, iface); backend != nil || err != nil {
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

// validateInterfaceName rejects names that could escape a config directory or be
// parsed as an option by ifup/ifdown. "." and ":" stay valid for VLAN and alias names.
func validateInterfaceName(iface string) error {
	if strings.TrimSpace(iface) == "" {
		return fmt.Errorf("interface name is required")
	}
	if len(iface) > 15 || strings.HasPrefix(iface, "-") || strings.ContainsFunc(iface, func(r rune) bool {
		return r == '/' || unicode.IsSpace(r) || unicode.IsControl(r)
	}) {
		return fmt.Errorf("invalid interface name %q", iface)
	}
	return nil
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

func detectIfcfgBackend(env Environment, iface string) (ConfigBackend, error) {
	path := filepath.Join(env.IfcfgDir, "ifcfg-"+iface)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return &ifcfgBackend{env: env, iface: iface, path: path}, nil
}
