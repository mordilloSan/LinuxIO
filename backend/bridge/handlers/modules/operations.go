package modules

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/go_logger/logger"
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
	// Validate source path exists
	sourcePath = filepath.Clean(sourcePath)
	if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("source path does not exist: %s", sourcePath)
	}

	// Parse and validate manifest
	manifestPath := filepath.Join(sourcePath, "module.yaml")
	manifest, err := parseManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("invalid module manifest: %w", err)
	}

	// Use manifest name if targetName not provided
	if targetName == "" {
		targetName = manifest.Name
	}

	// Sanitize target name (prevent path traversal)
	targetName = filepath.Base(targetName)

	logger.Infof("Installing module: %s (source=%s, symlink=%v)", targetName, sourcePath, createSymlink)

	// Check for conflicts
	if _, exists := GetModule(targetName); exists {
		return nil, fmt.Errorf("module '%s' already exists", targetName)
	}

	// Determine target path (user modules directory)
	userHome := os.Getenv("HOME")
	if userHome == "" {
		userHome = "/root"
	}
	userModulesDir := filepath.Join(userHome, ".config/linuxio/modules")

	// Ensure user modules directory exists
	if err := os.MkdirAll(userModulesDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create user modules directory: %w", err)
	}

	targetDir := filepath.Join(userModulesDir, targetName)

	// Create target (copy or symlink)
	if createSymlink {
		absSource, err := filepath.Abs(sourcePath)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve absolute path: %w", err)
		}

		// Validate symlink target is not pointing to sensitive system directories
		// Resolve any symlinks in the source path to get the real target
		realSource, err := filepath.EvalSymlinks(absSource)
		if err != nil {
			// If EvalSymlinks fails, use the absolute path but log warning
			logger.Warnf("Could not resolve symlinks in source path: %v", err)
			realSource = absSource
		}

		// Block symlinks to sensitive system directories
		sensitivePrefix := []string{"/etc/", "/var/", "/usr/", "/bin/", "/sbin/", "/lib/", "/root/", "/boot/", "/sys/", "/proc/"}
		for _, prefix := range sensitivePrefix {
			if strings.HasPrefix(realSource, prefix) && !strings.HasPrefix(realSource, "/etc/linuxio/") && !strings.HasPrefix(realSource, "/var/lib/linuxio/") {
				return nil, fmt.Errorf("symlink to sensitive system path not allowed: %s", realSource)
			}
		}

		if err := os.Symlink(absSource, targetDir); err != nil {
			return nil, fmt.Errorf("failed to create symlink: %w", err)
		}
		logger.Infof("Created symlink: %s -> %s", targetDir, absSource)
	} else {
		// Copy directory recursively
		if err := copyDir(sourcePath, targetDir); err != nil {
			return nil, fmt.Errorf("failed to copy module: %w", err)
		}
		logger.Infof("Copied module to: %s", targetDir)
	}

	// Load and register the module
	module := &ModuleInfo{
		Manifest: *manifest,
		Path:     targetDir,
		Enabled:  true,
	}

	// Register module handlers
	if err := registerModule(module, streamHandlers); err != nil {
		// Cleanup on failure
		_ = os.RemoveAll(targetDir)
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
