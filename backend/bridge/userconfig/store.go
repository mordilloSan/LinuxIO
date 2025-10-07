package userconfig

import "path/filepath"

// Save writes the given settings atomically for `username` and
// returns the absolute path used. It respects Homedir() + fallbacks
// and keeps file perms consistent.
func Save(username string, cfg *Settings) (string, error) {
	base, err := Homedir(username)
	if err != nil {
		if base, err = fallbackBase(username); err != nil {
			return "", err
		}
	}
	cfgPath := filepath.Join(base, cfgFileName)
	if err := writeConfigFrom(cfgPath, *cfg); err != nil {
		return "", err
	}
	_ = ensureFilePerms(cfgPath, filePerm)
	return cfgPath, nil
}

// Load returns the parsed Settings for `username` and the absolute config path.
// It does NOT create/repair the file; call Initialize(username) first if needed.
func Load(username string) (*Settings, string, error) {
	base, err := Homedir(username)
	if err != nil {
		// fall back if no home (same logic as Initialize)
		if base, err = fallbackBase(username); err != nil {
			return nil, "", err
		}
	}
	cfgPath := filepath.Join(base, cfgFileName)

	// strict read (unknown keys rejected); your repair path runs in Initialize.
	cfg, err := readConfigStrict(cfgPath)
	if err != nil {
		return nil, "", err
	}
	return cfg, cfgPath, nil
}
