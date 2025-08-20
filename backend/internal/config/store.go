package config

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
