package modules

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// IsSystemModule checks if a module path is in the system directory (/etc/linuxio/modules/)
func IsSystemModule(modulePath string) bool {
	cleanPath := filepath.Clean(modulePath)
	return strings.HasPrefix(cleanPath, "/etc/linuxio/modules/")
}

// IsSymlinkModule checks if a module path is a symlink
func IsSymlinkModule(modulePath string) (bool, error) {
	info, err := os.Lstat(modulePath)
	if err != nil {
		return false, err
	}
	return info.Mode()&os.ModeSymlink != 0, nil
}

// UninstallModuleOperation removes a module from the filesystem and registry
func UninstallModuleOperation(
	moduleName string,
	streamHandlers map[string]func(*session.Session, net.Conn, []string) error,
) (*UninstallResult, error) {
	module, exists := GetModule(moduleName)
	if !exists {
		return nil, fmt.Errorf("module '%s' not found", moduleName)
	}

	logger.Infof("Uninstalling module: %s (path=%s)", moduleName, module.Path)

	// Unregister from new handler system
	namespace := "module." + moduleName
	ipc.UnregisterAll(namespace)

	// Remove stream handlers for this module
	for streamType := range streamHandlers {
		if strings.HasPrefix(streamType, namespace+".") {
			delete(streamHandlers, streamType)
		}
	}

	// Remove from loaded modules
	delete(loadedModules, moduleName)

	// Remove from filesystem
	if err := os.RemoveAll(module.Path); err != nil {
		return nil, fmt.Errorf("failed to remove module directory: %w", err)
	}
	logger.Infof("Removed module directory: %s", module.Path)

	return &UninstallResult{
		Success: true,
		Message: fmt.Sprintf("Module '%s' uninstalled successfully", moduleName),
	}, nil
}

// InstallModuleOperation installs a module from a source path
func InstallModuleOperation(
	sourcePath, targetName string,
	createSymlink bool,
	streamHandlers map[string]func(*session.Session, net.Conn, []string) error,
) (*InstallResult, error) {
	sourcePath = filepath.Clean(sourcePath)
	if err := validateModuleSourcePath(sourcePath); err != nil {
		return nil, err
	}

	manifest, err := parseManifest(filepath.Join(sourcePath, "module.yaml"))
	if err != nil {
		return nil, fmt.Errorf("invalid module manifest: %w", err)
	}

	targetName = resolveInstallModuleTargetName(targetName, manifest.Name)

	logger.Infof("Installing module: %s (source=%s, symlink=%v)", targetName, sourcePath, createSymlink)

	if _, exists := GetModule(targetName); exists {
		return nil, fmt.Errorf("module '%s' already exists", targetName)
	}

	targetDir, err := moduleInstallTargetDir(targetName)
	if err != nil {
		return nil, err
	}
	if err := createInstalledModuleTarget(sourcePath, targetDir, createSymlink); err != nil {
		return nil, err
	}

	module := &ModuleInfo{
		Manifest: *manifest,
		Path:     targetDir,
		Enabled:  true,
	}

	if err := registerModule(module, streamHandlers); err != nil {
		if removeErr := os.RemoveAll(targetDir); removeErr != nil {
			logger.Warnf("failed to cleanup module directory after register error (%s): %v", targetDir, removeErr)
		}
		return nil, fmt.Errorf("failed to register module: %w", err)
	}

	loadedModules[targetName] = module
	logger.Infof("Module '%s' installed and loaded successfully", targetName)

	return &InstallResult{
		Success:    true,
		ModuleName: targetName,
		Message:    fmt.Sprintf("Module '%s' v%s installed successfully", manifest.Title, manifest.Version),
	}, nil
}

func validateModuleSourcePath(sourcePath string) error {
	if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
		return fmt.Errorf("source path does not exist: %s", sourcePath)
	}
	return nil
}

func resolveInstallModuleTargetName(targetName, manifestName string) string {
	if targetName == "" {
		targetName = manifestName
	}
	return filepath.Base(targetName)
}

func moduleInstallTargetDir(targetName string) (string, error) {
	userHome := os.Getenv("HOME")
	if userHome == "" {
		userHome = "/root"
	}
	userModulesDir := filepath.Join(userHome, ".config/linuxio/modules")
	if err := os.MkdirAll(userModulesDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create user modules directory: %w", err)
	}
	return filepath.Join(userModulesDir, targetName), nil
}

func createInstalledModuleTarget(sourcePath, targetDir string, createSymlink bool) error {
	if createSymlink {
		return createInstalledModuleSymlink(sourcePath, targetDir)
	}
	if err := copyDir(sourcePath, targetDir); err != nil {
		return fmt.Errorf("failed to copy module: %w", err)
	}
	logger.Infof("Copied module to: %s", targetDir)
	return nil
}

func createInstalledModuleSymlink(sourcePath, targetDir string) error {
	absSource, err := filepath.Abs(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute path: %w", err)
	}
	realSource, err := filepath.EvalSymlinks(absSource)
	if err != nil {
		logger.Warnf("Could not resolve symlinks in source path: %v", err)
		realSource = absSource
	}
	if err := validateModuleSymlinkTarget(realSource); err != nil {
		return err
	}
	if err := os.Symlink(absSource, targetDir); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}
	logger.Infof("Created symlink: %s -> %s", targetDir, absSource)
	return nil
}

func validateModuleSymlinkTarget(realSource string) error {
	sensitivePrefixes := []string{"/etc/", "/var/", "/usr/", "/bin/", "/sbin/", "/lib/", "/root/", "/boot/", "/sys/", "/proc/"}
	for _, prefix := range sensitivePrefixes {
		if strings.HasPrefix(realSource, prefix) &&
			!strings.HasPrefix(realSource, "/etc/linuxio/") &&
			!strings.HasPrefix(realSource, "/var/lib/linuxio/") {
			return fmt.Errorf("symlink to sensitive system path not allowed: %s", realSource)
		}
	}
	return nil
}

// ValidateModuleAtPath validates a module.yaml file without installing
func ValidateModuleAtPath(path string) (*ValidationResult, error) {
	path = filepath.Clean(path)
	manifestPath := filepath.Join(path, "module.yaml")

	// Check if manifest exists
	manifestInfo, statErr := os.Stat(manifestPath)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return &ValidationResult{
				Valid:  false,
				Errors: []string{"module.yaml not found in specified path"},
			}, nil
		}
		return &ValidationResult{
			Valid:  false,
			Errors: []string{fmt.Sprintf("Failed to stat module.yaml: %v", statErr)},
		}, nil
	}
	if manifestInfo.IsDir() {
		return &ValidationResult{
			Valid:  false,
			Errors: []string{"module.yaml is a directory"},
		}, nil
	}

	// Parse manifest
	manifest, err := parseManifest(manifestPath)
	if err != nil {
		return &ValidationResult{
			Valid:  false,
			Errors: []string{fmt.Sprintf("Failed to parse module.yaml: %v", err)},
		}, nil
	}

	// Validate required fields
	var errors []string
	if manifest.Name == "" {
		errors = append(errors, "Missing required field: name")
	}
	if manifest.Version == "" {
		errors = append(errors, "Missing required field: version")
	}
	if manifest.Title == "" {
		errors = append(errors, "Missing required field: title")
	}

	// Validate name format (no path separators)
	if strings.Contains(manifest.Name, "/") || strings.Contains(manifest.Name, "\\") {
		errors = append(errors, "Module name cannot contain path separators")
	}

	if len(errors) > 0 {
		return &ValidationResult{
			Valid:    false,
			Errors:   errors,
			Manifest: manifest,
		}, nil
	}

	return &ValidationResult{
		Valid:    true,
		Errors:   []string{},
		Manifest: manifest,
	}, nil
}

// copyDir recursively copies a directory, skipping symlinks for security
func copyDir(src, dst string) error {
	// Get source directory info (using Lstat to not follow symlinks)
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return err
	}

	// Skip if source is a symlink
	if srcInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("source path is a symlink, refusing to copy: %s", src)
	}

	// Create destination directory
	err = os.MkdirAll(dst, srcInfo.Mode())
	if err != nil {
		return err
	}

	// Read directory contents
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		// Check if entry is a symlink and skip it
		if entry.Type()&os.ModeSymlink != 0 {
			logger.Warnf("Skipping symlink during copy: %s", srcPath)
			continue
		}

		if entry.IsDir() {
			// Recursively copy subdirectory
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			// Copy file
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// copyFile copies a single file
func copyFile(src, dst string) error {
	// Open source file
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Get source file info for permissions
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	// Create destination file
	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	// Copy contents
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	return dstFile.Sync()
}
