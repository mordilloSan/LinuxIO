package packages

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
)

// claudeCodeComponent is the InstallSpec.OptionalComponent value that adds
// Anthropic's package repository. The capability's package step then installs
// claude-code from it through PackageKit, and later versions arrive with the
// host's ordinary package updates instead of updating themselves.
const claudeCodeComponent = "claude_code"

// claudeCodeKeyFingerprint is the release signing key Anthropic publishes
// beside its install instructions (https://code.claude.com/docs/en/setup).
const claudeCodeKeyFingerprint = "31DDDE24DDFAB679F42D7BD2BAA929FF1A7ECACE"

// claudeCodeChannel is the release channel the repository follows: "latest"
// offers every release as it ships, "stable" trails it by about a week.
const claudeCodeChannel = "latest"

var (
	claudeCodeKeyURL = "https://downloads.claude.ai/keys/claude-code.asc"
	// claudeCodeRoot prefixes the files the repository setup writes; tests
	// point it at a temporary directory.
	claudeCodeRoot                = "/"
	capabilityRefreshPackageCache = RefreshUpdateCache
)

// claudeCodeRepository is where one package manager keeps Anthropic's
// repository definition and signing key.
type claudeCodeRepository struct {
	// configDir exists on every host that uses this package manager.
	configDir string
	keyPath   string
	repoPath  string
	repo      string
	// importKey is set for rpm, which keeps trusted keys in its own database.
	importKey bool
}

func claudeCodeRepositoryFor(family string) claudeCodeRepository {
	if isRHELFamily(family) {
		const key = "/etc/pki/rpm-gpg/RPM-GPG-KEY-claude-code"
		return claudeCodeRepository{
			configDir: "/etc/yum.repos.d",
			keyPath:   key,
			repoPath:  "/etc/yum.repos.d/claude-code.repo",
			repo: "[claude-code]\n" +
				"name=Claude Code\n" +
				"baseurl=https://downloads.claude.ai/claude-code/rpm/" + claudeCodeChannel + "\n" +
				"enabled=1\n" +
				"gpgcheck=1\n" +
				"gpgkey=file://" + key + "\n",
			importKey: true,
		}
	}
	const key = "/etc/apt/keyrings/claude-code.asc"
	return claudeCodeRepository{
		configDir: "/etc/apt",
		keyPath:   key,
		repoPath:  "/etc/apt/sources.list.d/claude-code.list",
		repo: fmt.Sprintf("deb [signed-by=%s] https://downloads.claude.ai/claude-code/apt/%s %s main\n",
			key, claudeCodeChannel, claudeCodeChannel),
	}
}

// installClaudeCodeRepository makes Anthropic's repository known to the
// package manager and refreshes the package lists so claude-code resolves. A
// repository file that already exists is left alone: an administrator who
// configured one chose its channel.
func installClaudeCodeRepository(ctx context.Context, task *bridgeipc.Task) error {
	repo := claudeCodeRepositoryFor(capabilityDistroFamily())
	rooted := func(path string) string { return filepath.Join(claudeCodeRoot, path) }

	if info, err := os.Stat(rooted(repo.configDir)); err != nil || !info.IsDir() {
		return fmt.Errorf("no Claude Code package repository for this distribution (%s not found)", repo.configDir)
	}
	_, statErr := os.Stat(rooted(repo.repoPath))
	switch {
	case statErr == nil:
		reportProgress(task, stageResolve, fmt.Sprintf("Anthropic's repository is already configured in %s; leaving it unchanged", repo.repoPath), pctResolve)
	case errors.Is(statErr, fs.ErrNotExist):
		if err := addClaudeCodeRepository(ctx, task, repo, rooted); err != nil {
			return err
		}
	default:
		return fmt.Errorf("check %s: %w", repo.repoPath, statErr)
	}

	reportProgress(task, stageResolve, "Refreshing the package lists", pctResolve)
	if err := capabilityRefreshPackageCache(ctx); err != nil {
		return fmt.Errorf("refresh package lists: %w", err)
	}
	return nil
}

// addClaudeCodeRepository downloads Anthropic's signing key, refuses it
// unless it is exactly the published key, and only then writes the key and
// the repository definition.
func addClaudeCodeRepository(ctx context.Context, task *bridgeipc.Task, repo claudeCodeRepository, rooted func(string) string) error {
	reportProgress(task, stageResolve, fmt.Sprintf("Downloading Anthropic's signing key from %s", claudeCodeKeyURL), pctResolve)
	key, err := fetchInstallFile(ctx, claudeCodeKeyURL)
	if err != nil {
		return fmt.Errorf("download Claude Code signing key: %w", err)
	}
	if err := singlePinnedFingerprint(key, claudeCodeKeyFingerprint); err != nil {
		return fmt.Errorf("verify Claude Code signing key: %w", err)
	}
	reportProgress(task, stageResolve, fmt.Sprintf("Signing key verified (fingerprint %s)", claudeCodeKeyFingerprint), pctResolve)

	if err := atomicfile.Write(rooted(repo.keyPath), key, 0o644); err != nil {
		return fmt.Errorf("store Claude Code signing key: %w", err)
	}
	if repo.importKey {
		const message = "Trusting the signing key in the rpm database"
		reportProgress(task, stageResolve, message, pctResolve)
		if err := runCapabilityCommand(ctx, "rpm", []string{"--import", rooted(repo.keyPath)}, func(output InstallCapabilityOutput) {
			reportOutput(task, stageResolve, message, pctResolve, output)
		}); err != nil {
			return fmt.Errorf("import Claude Code signing key: %w", err)
		}
	}
	if err := atomicfile.Write(rooted(repo.repoPath), []byte(repo.repo), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", repo.repoPath, err)
	}
	reportProgress(task, stageResolve, fmt.Sprintf("Added Anthropic's %s repository in %s", claudeCodeChannel, repo.repoPath), pctResolve)
	slog.Info("package repository added", "repository", "claude-code", "channel", claudeCodeChannel, "path", repo.repoPath)
	return nil
}
