package userconfig

import (
	"errors"
	"io/fs"
	"os"

	"github.com/goccy/go-yaml"
)

// regularFileExists reports whether path is an existing regular file. A symlink is an error.
func regularFileExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return false, errors.New("config path must not be a symlink")
		}
		return info.Mode().IsRegular(), nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func writeCoreConfigOwned(cfgPath string, cfg Settings, owner fileOwnership) error {
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}
	return owner.writeAtomic(cfgPath, data, filePerm)
}

func writeUIConfigOwned(uiPath string, ui UIPreferences, owner fileOwnership) error {
	data, err := yaml.Marshal(&ui)
	if err != nil {
		return err
	}
	return owner.writeAtomic(uiPath, data, filePerm)
}

func writeEmptyUIConfigOwned(uiPath string, owner fileOwnership) error {
	return owner.writeAtomic(uiPath, []byte("{}\n"), filePerm)
}
