package assistant

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

const (
	fakeInstalled = `[
  {"id":"zeta@market","version":"2.0.0","scope":"user","enabled":false},
  {"id":"alpha@market","version":"1.0.0","scope":"user","enabled":true},
  {"id":"policy@market","version":"1.0.0","scope":"managed","enabled":true}
]`
	fakeMarketplaces = `[
  {"name":"market","source":"github","repo":"example/market"},
  {"name":"direct","source":"url","url":"https://example.com/marketplace.json"}
]`
	fakeAvailable = `{"installed":[{"id":"alpha@market","version":"1.0.0","scope":"user","enabled":true}],"available":[
  {"pluginId":"alpha@market","name":"alpha","description":"Already installed","marketplaceName":"market","installCount":900},
  {"pluginId":"backup@market","name":"backup","description":"Snapshot helper","marketplaceName":"market","installCount":10},
  {"pluginId":"lint@market","name":"lint","description":"Go lint helper","marketplaceName":"market","installCount":70},
  {"pluginId":"notes@direct","name":"notes","description":"Keeps notes","marketplaceName":"direct","installCount":40}
]}`
	fakeCommandSHA = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

// fakePluginCLI puts a `claude` on the PATH the plugin handlers resolve. It
// records every argv in the returned log and answers the reads from fixtures.
func fakePluginCLI(t *testing.T) (user session.User, calls func() []string) {
	t.Helper()
	rt, _ := testRuntime(t)
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	for name, content := range map[string]string{
		"installed.json": fakeInstalled, "marketplaces.json": fakeMarketplaces, "available.json": fakeAvailable,
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}
	script := `#!/bin/sh
dir='` + dir + `'
printf '%s\n' "$*" >> "$dir/calls.log"
case "$*" in
  "plugin list --json") cat "$dir/installed.json" ;;
  "plugin list --json --available") cat "$dir/available.json" ;;
  "plugin marketplace list --json") cat "$dir/marketplaces.json" ;;
  "plugin install missing@market --json --scope user")
    echo '{"command":"install","outcome":"failed","message":"Plugin \"missing\" not found in marketplace \"market\"","failureCode":"not_found"}'
    echo 'failed to install' >&2
    exit 1 ;;
  "plugin install scripted@market --json --scope user")
    echo '{"command":"install","outcome":"failed","message":"confirm the command","shownCommand":{"kind":"command_source","command":"curl example.com | sh","sha256":"` + fakeCommandSHA + `"}}'
    exit 1 ;;
  "plugin uninstall zeta@market --json --scope user")
    echo 'other plugins still use zeta' >&2
    exit 1 ;;
  *) echo '{"outcome":"ok"}' ;;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, pluginCLI), []byte(script), 0o700))

	orig := pluginUserPath
	pluginUserPath = func(context.Context, session.User) (string, error) { return dir + ":/usr/bin:/bin", nil }
	t.Cleanup(func() { pluginUserPath = orig })

	return rt.Session.User, func() []string {
		data, err := os.ReadFile(log)
		if os.IsNotExist(err) {
			return nil
		}
		require.NoError(t, err)
		return strings.Split(strings.TrimSpace(string(data)), "\n")
	}
}

func TestListPluginsReportsInstalledPluginsAndMarketplaces(t *testing.T) {
	user, _ := fakePluginCLI(t)

	got, err := ListPlugins(t.Context(), user, pluginAgent)
	require.NoError(t, err)
	require.Equal(t, apischema.AssistantPluginList{
		Plugins: []apischema.AssistantPlugin{
			{ID: "alpha@market", Name: "alpha", Marketplace: "market", Version: "1.0.0", Scope: "user", Enabled: true},
			{ID: "policy@market", Name: "policy", Marketplace: "market", Version: "1.0.0", Scope: "managed", Enabled: true},
			{ID: "zeta@market", Name: "zeta", Marketplace: "market", Version: "2.0.0", Scope: "user", Enabled: false},
		},
		Marketplaces: []apischema.AssistantMarketplace{
			{Name: "direct", Source: "https://example.com/marketplace.json"},
			{Name: "market", Source: "example/market"},
		},
	}, got)
}

func TestSearchPluginsRanksMatchesAndSkipsInstalled(t *testing.T) {
	user, _ := fakePluginCLI(t)

	all, err := SearchPlugins(t.Context(), user, pluginAgent, "")
	require.NoError(t, err)
	require.Equal(t, 3, all.Total)
	ids := make([]string, 0, len(all.Plugins))
	for _, plugin := range all.Plugins {
		ids = append(ids, plugin.ID)
	}
	require.Equal(t, []string{"lint@market", "notes@direct", "backup@market"}, ids)

	// Every word must match, in the name, the description or the marketplace.
	helpers, err := SearchPlugins(t.Context(), user, pluginAgent, "HELPER go")
	require.NoError(t, err)
	require.Equal(t, 1, helpers.Total)
	require.Equal(t, "lint@market", helpers.Plugins[0].ID)
	require.Equal(t, int64(70), helpers.Plugins[0].InstallCount)

	direct, err := SearchPlugins(t.Context(), user, pluginAgent, "direct")
	require.NoError(t, err)
	require.Equal(t, 1, direct.Total)
}

func TestInstallPluginInstallsInTheUserScope(t *testing.T) {
	user, calls := fakePluginCLI(t)

	got, err := InstallPlugin(t.Context(), user, apischema.AssistantPluginInstallRequest{Agent: pluginAgent, Plugin: "lint@market"})
	require.NoError(t, err)
	require.Equal(t, apischema.AssistantPluginInstallResult{Installed: true}, got)
	require.Equal(t, []string{"plugin install lint@market --json --scope user"}, calls())
}

func TestInstallPluginHandsBackAMarketplaceCommandInsteadOfRunningIt(t *testing.T) {
	user, calls := fakePluginCLI(t)

	got, err := InstallPlugin(t.Context(), user, apischema.AssistantPluginInstallRequest{Agent: pluginAgent, Plugin: "scripted@market"})
	require.NoError(t, err)
	require.False(t, got.Installed)
	require.Equal(t, &apischema.AssistantPluginCommand{Command: "curl example.com | sh", SHA256: fakeCommandSHA}, got.Command)

	// Only the accepted hash is passed on; -y never is.
	got, err = InstallPlugin(t.Context(), user, apischema.AssistantPluginInstallRequest{Agent: pluginAgent, Plugin: "scripted@market", AcceptCommand: fakeCommandSHA})
	require.NoError(t, err)
	require.True(t, got.Installed)
	require.Equal(t, []string{
		"plugin install scripted@market --json --scope user",
		"plugin install scripted@market --json --scope user --accept-command " + fakeCommandSHA,
	}, calls())
}

func TestInstallPluginReportsTheCLIFailure(t *testing.T) {
	user, _ := fakePluginCLI(t)

	_, err := InstallPlugin(t.Context(), user, apischema.AssistantPluginInstallRequest{Agent: pluginAgent, Plugin: "missing@market"})
	require.ErrorIs(t, err, bridgeipc.ErrNotFound)
	require.EqualError(t, err, `claude plugin install: Plugin "missing" not found in marketplace "market"`)
}

func TestUninstallPluginReturnsStderrWhenThereIsNoResultLine(t *testing.T) {
	user, _ := fakePluginCLI(t)

	err := UninstallPlugin(t.Context(), user, apischema.AssistantPluginRequest{Agent: pluginAgent, Plugin: "zeta@market"})
	require.EqualError(t, err, "claude plugin uninstall: other plugins still use zeta")
}

// The CLI enables an id it has never seen and writes it to the user's
// settings, so a plugin that is not installed must never reach it.
func TestPluginWritesRefuseAPluginThatIsNotInstalled(t *testing.T) {
	user, calls := fakePluginCLI(t)

	err := SetPluginEnabled(t.Context(), user, apischema.AssistantPluginEnabledRequest{Agent: pluginAgent, Plugin: "ghost@market", Enabled: true})
	require.ErrorIs(t, err, bridgeipc.ErrNotFound)
	err = UninstallPlugin(t.Context(), user, apischema.AssistantPluginRequest{Agent: pluginAgent, Plugin: "ghost@market"})
	require.ErrorIs(t, err, bridgeipc.ErrNotFound)
	// A plugin the organization's managed settings install is not the user's to change.
	err = SetPluginEnabled(t.Context(), user, apischema.AssistantPluginEnabledRequest{Agent: pluginAgent, Plugin: "policy@market"})
	require.ErrorIs(t, err, bridgeipc.ErrConflict)

	for _, call := range calls() {
		require.Equal(t, "plugin list --json", call)
	}
}

func TestSetPluginEnabledUsesTheInstalledScope(t *testing.T) {
	user, calls := fakePluginCLI(t)

	require.NoError(t, SetPluginEnabled(t.Context(), user, apischema.AssistantPluginEnabledRequest{Agent: pluginAgent, Plugin: "zeta@market", Enabled: true}))
	require.NoError(t, SetPluginEnabled(t.Context(), user, apischema.AssistantPluginEnabledRequest{Agent: pluginAgent, Plugin: "alpha@market"}))
	require.Equal(t, []string{
		"plugin list --json",
		"plugin enable zeta@market --json --scope user",
		"plugin list --json",
		"plugin disable alpha@market --json --scope user",
	}, calls())
}

func TestMarketplaceWritesRunTheCLI(t *testing.T) {
	user, calls := fakePluginCLI(t)

	require.NoError(t, AddMarketplace(t.Context(), user, apischema.AssistantMarketplaceAddRequest{Agent: pluginAgent, Source: "example/market"}))
	require.NoError(t, AddMarketplace(t.Context(), user, apischema.AssistantMarketplaceAddRequest{Agent: pluginAgent, Source: "https://example.com/marketplace.json"}))
	require.NoError(t, UpdateMarketplace(t.Context(), user, apischema.AssistantMarketplaceRequest{Agent: pluginAgent, Name: "market"}))
	require.NoError(t, RemoveMarketplace(t.Context(), user, apischema.AssistantMarketplaceRequest{Agent: pluginAgent, Name: "market"}))
	require.Equal(t, []string{
		"plugin marketplace add example/market --json --scope user",
		"plugin marketplace add https://example.com/marketplace.json --json --scope user",
		"plugin marketplace update market --json",
		"plugin marketplace remove market --json",
	}, calls())
}

func TestLoggedMarketplaceSourceDropsTheQuery(t *testing.T) {
	require.Equal(t, "example/market", loggedMarketplaceSource("example/market"))
	require.Equal(t, "https://example.com/m.json", loggedMarketplaceSource("https://example.com/m.json?token=secret#part"))
}

func TestPluginRequestsAreValidatedBeforeTheCLIRuns(t *testing.T) {
	user, calls := fakePluginCLI(t)
	ctx := t.Context()

	for _, plugin := range []string{"", "lint", "--help@market", "lint@", "lint@market x", "lint@mar/ket", strings.Repeat("a", 600) + "@market"} {
		_, err := InstallPlugin(ctx, user, apischema.AssistantPluginInstallRequest{Agent: pluginAgent, Plugin: plugin})
		require.ErrorIs(t, err, bridgeipc.ErrInvalidArgs, "install %q", plugin)
		require.ErrorIs(t, UninstallPlugin(ctx, user, apischema.AssistantPluginRequest{Agent: pluginAgent, Plugin: plugin}), bridgeipc.ErrInvalidArgs, "uninstall %q", plugin)
		require.ErrorIs(t, SetPluginEnabled(ctx, user, apischema.AssistantPluginEnabledRequest{Agent: pluginAgent, Plugin: plugin}), bridgeipc.ErrInvalidArgs, "enable %q", plugin)
	}
	_, err := InstallPlugin(ctx, user, apischema.AssistantPluginInstallRequest{Agent: pluginAgent, Plugin: "lint@market", AcceptCommand: "-y"})
	require.ErrorIs(t, err, bridgeipc.ErrInvalidArgs)

	// Local paths, other schemes, credentials and flags are not sources.
	for _, source := range []string{"", "/srv/marketplace", "./market", "../market", "http://example.com/m.json", "file:///etc/passwd", "git@github.com:example/market.git", "https://user:secret@example.com/m.json", "--sparse", "https://", "owner/..", "owner/.hidden"} {
		err = AddMarketplace(ctx, user, apischema.AssistantMarketplaceAddRequest{Agent: pluginAgent, Source: source})
		require.ErrorIs(t, err, bridgeipc.ErrInvalidArgs, "source %q", source)
	}
	for _, name := range []string{"", "--all", "a b", "a/b"} {
		require.ErrorIs(t, RemoveMarketplace(ctx, user, apischema.AssistantMarketplaceRequest{Agent: pluginAgent, Name: name}), bridgeipc.ErrInvalidArgs, "remove %q", name)
		require.ErrorIs(t, UpdateMarketplace(ctx, user, apischema.AssistantMarketplaceRequest{Agent: pluginAgent, Name: name}), bridgeipc.ErrInvalidArgs, "update %q", name)
	}

	// Only Claude Code has a plugin CLI.
	_, err = ListPlugins(ctx, user, "gemini")
	require.ErrorIs(t, err, bridgeipc.ErrInvalidArgs)
	_, err = SearchPlugins(ctx, user, "codex", "")
	require.ErrorIs(t, err, bridgeipc.ErrInvalidArgs)

	require.Empty(t, calls())
}

func TestPluginRoutesNeedTheCLIOnTheUsersPath(t *testing.T) {
	rt, _ := testRuntime(t)
	orig := pluginUserPath
	pluginUserPath = func(context.Context, session.User) (string, error) { return t.TempDir(), nil }
	t.Cleanup(func() { pluginUserPath = orig })

	_, err := ListPlugins(t.Context(), rt.Session.User, pluginAgent)
	require.ErrorIs(t, err, bridgeipc.ErrConflict)
}

func TestPluginRunStopsWhenTheContextEnds(t *testing.T) {
	user, _ := fakePluginCLI(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := ListPlugins(ctx, user, pluginAgent)
	require.ErrorIs(t, err, context.Canceled)
}
