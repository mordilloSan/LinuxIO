package config

import "os"

func normalFileOwnership() fileOwnership {
	return fileOwnership{}
}

func currentProcessFileOwnership() fileOwnership {
	return fileOwnership{uid: os.Geteuid(), gid: os.Getegid(), enforce: true}
}

func initializeLocked(cfgPath, uiPath, base string) error {
	return initializeLockedOwned(cfgPath, uiPath, base, normalFileOwnership())
}

func readUILatest(path string) (*UIPreferences, error) {
	return readUILatestOwned(path, normalFileOwnership())
}

func writeCoreConfig(cfgPath string, cfg Settings) error {
	return writeCoreConfigOwned(cfgPath, cfg, normalFileOwnership())
}

func writeUIConfig(uiPath string, ui UIPreferences) error {
	return writeUIConfigOwned(uiPath, ui, normalFileOwnership())
}
