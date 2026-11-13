package userconfig

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"reflect"
	"strings"

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

// writeUIConfigOwned stores only what differs from DefaultUIPreferences, so a
// default the backend changes later still reaches a user who has saved before.
// parseUIConfig reads the file back over the defaults.
func writeUIConfigOwned(uiPath string, ui UIPreferences, owner fileOwnership) error {
	document, err := sparseUIDocument(ui)
	if err != nil {
		return err
	}
	if len(document) == 0 {
		return writeEmptyUIConfigOwned(uiPath, owner)
	}
	data, err := yaml.Marshal(document)
	if err != nil {
		return err
	}
	return owner.writeAtomic(uiPath, data, filePerm)
}

// sparseUIDocument returns the fields of ui that differ from the defaults, in
// the order of UIPreferences. It walks the struct, so a field added to it is
// stored without being named here. Theme colours differ per key: see
// sparseThemeColors.
func sparseUIDocument(ui UIPreferences) (yaml.MapSlice, error) {
	defaults := DefaultUIPreferences()
	value, defaultValue := reflect.ValueOf(ui), reflect.ValueOf(defaults)
	document := yaml.MapSlice{}
	for i := range value.NumField() {
		name, _, _ := strings.Cut(value.Type().Field(i).Tag.Get("yaml"), ",")
		if name == "themeColors" {
			if colors := sparseThemeColors(ui.ThemeColors, defaults.ThemeColors); len(colors) > 0 {
				document = append(document, yaml.MapItem{Key: name, Value: colors})
			}
			continue
		}
		field, defaultField := value.Field(i).Interface(), defaultValue.Field(i).Interface()
		// Compare the encodings: an empty list and a nil one are the same document.
		encoded, err := yaml.Marshal(field)
		if err != nil {
			return nil, err
		}
		defaultEncoded, err := yaml.Marshal(defaultField)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(encoded, defaultEncoded) {
			document = append(document, yaml.MapItem{Key: name, Value: field})
		}
	}
	return document, nil
}

func writeEmptyUIConfigOwned(uiPath string, owner fileOwnership) error {
	return owner.writeAtomic(uiPath, []byte("{}\n"), filePerm)
}
