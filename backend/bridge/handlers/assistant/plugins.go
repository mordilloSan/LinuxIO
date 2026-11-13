package assistant

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/usercmd"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// Plugins are managed the way Claude Code's own editors manage them: by
// running `claude plugin ... --json` as the session user, never by editing
// the agent's settings files. Only Claude Code has such a CLI.
const (
	pluginAgent = "claude"
	pluginCLI   = "claude"

	pluginReadTimeout  = 30 * time.Second
	pluginWriteTimeout = 10 * time.Minute
	// pluginOutputMax bounds what one CLI run may print; the whole catalogue
	// is a few megabytes.
	pluginOutputMax = 32 << 20
	// pluginSearchLimit is how many catalogue entries one search returns.
	pluginSearchLimit = 50
	pluginKillDelay   = 5 * time.Second
)

var (
	// pluginUserPath resolves the user's login-shell PATH. It is a seam so
	// tests can point the handlers at a fake CLI.
	pluginUserPath = usercmd.Path

	// A leading letter or digit keeps every value from reading as a flag.
	pluginIDPattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*@[A-Za-z0-9][A-Za-z0-9._-]*$`)
	marketplaceNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	// The repository starts with a letter or digit too, so "owner/.." and
	// other relative paths are never taken for one.
	githubRepoPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9][A-Za-z0-9._-]*$`)
	commandSHAPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

	pluginScopes = []string{"user", "project", "local"}
)

const pluginValueMax = 512

// pluginRunner runs the agent's plugin CLI for one user.
type pluginRunner struct {
	user session.User
	path string
	bin  string
}

func newPluginRunner(ctx context.Context, user session.User, agent string) (pluginRunner, error) {
	if agent != pluginAgent {
		return pluginRunner{}, fmt.Errorf("%w: agent %q has no plugins LinuxIO can manage", bridgeipc.ErrInvalidArgs, agent)
	}
	if user.Username == "" {
		return pluginRunner{}, errors.New("no session user to manage plugins for")
	}
	path, err := pluginUserPath(ctx, user)
	if err != nil {
		return pluginRunner{}, err
	}
	bin, err := usercmd.LookPathIn(path, pluginCLI)
	if err != nil {
		return pluginRunner{}, fmt.Errorf("%w: %w", bridgeipc.ErrConflict, err)
	}
	return pluginRunner{user: user, path: path, bin: bin}, nil
}

// cappedBuffer refuses output past max instead of growing without bound.
type cappedBuffer struct {
	bytes.Buffer
	max int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		return 0, fmt.Errorf("output exceeds %d bytes", b.max)
	}
	return b.Buffer.Write(p)
}

// pluginOutcome is the result line the CLI prints for a --json write.
type pluginOutcome struct {
	Outcome      string `json:"outcome"`
	Message      string `json:"message"`
	FailureCode  string `json:"failureCode"`
	ShownCommand *struct {
		Command string `json:"command"`
		SHA256  string `json:"sha256"`
	} `json:"shownCommand"`
}

// pluginCLIError is a failed CLI run, with the result line when one was
// printed.
type pluginCLIError struct {
	verb    string
	outcome *pluginOutcome
	stderr  string
	err     error
}

func (e *pluginCLIError) Error() string {
	reason := strings.TrimSpace(e.stderr)
	if e.outcome != nil && e.outcome.Message != "" {
		reason = e.outcome.Message
	}
	if reason == "" {
		reason = e.err.Error()
	}
	return fmt.Sprintf("claude plugin %s: %s", e.verb, reason)
}

func (e *pluginCLIError) Unwrap() error {
	if e.outcome != nil {
		switch e.outcome.FailureCode {
		case "not_found", "not_installed":
			return bridgeipc.ErrNotFound
		}
	}
	return e.err
}

// lastOutcome returns the result line of a --json write: the last line of
// stdout that is a JSON object.
func lastOutcome(stdout []byte) *pluginOutcome {
	lines := bytes.Split(bytes.TrimSpace(stdout), []byte("\n"))
	for _, line := range slices.Backward(lines) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var outcome pluginOutcome
		if err := json.Unmarshal(line, &outcome); err == nil {
			return &outcome
		}
	}
	return nil
}

// run executes `claude plugin <args>` as the user, in their home and on their
// login-shell PATH, and returns its stdout. Standard input is /dev/null, so
// the CLI never prompts.
func (r pluginRunner) run(ctx context.Context, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd, err := usercmd.Command(ctx, r.user, r.bin, append([]string{"plugin"}, args...)...)
	if err != nil {
		return nil, err
	}
	cmd.Env = append(cmd.Env, "PATH="+r.path)
	stdout := &cappedBuffer{max: pluginOutputMax}
	stderr := &tailBuffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	// The command leads its own session, so a cancel reaches the git or
	// helper processes the CLI started as well.
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = pluginKillDelay

	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("claude plugin %s: %w", args[0], ctxErr)
		}
		return nil, &pluginCLIError{verb: args[0], outcome: lastOutcome(stdout.Bytes()), stderr: stderr.String(), err: err}
	}
	return stdout.Bytes(), nil
}

type cliInstalledPlugin struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Scope   string `json:"scope"`
	Enabled bool   `json:"enabled"`
}

type cliAvailablePlugin struct {
	PluginID        string `json:"pluginId"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	MarketplaceName string `json:"marketplaceName"`
	InstallCount    int64  `json:"installCount"`
}

type cliMarketplace struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Repo   string `json:"repo"`
	URL    string `json:"url"`
	Path   string `json:"path"`
}

func (r pluginRunner) installed(ctx context.Context) ([]cliInstalledPlugin, error) {
	out, err := r.run(ctx, pluginReadTimeout, "list", "--json")
	if err != nil {
		return nil, err
	}
	var plugins []cliInstalledPlugin
	if err := json.Unmarshal(out, &plugins); err != nil {
		return nil, fmt.Errorf("decode claude plugin list: %w", err)
	}
	return plugins, nil
}

// installedScope returns the scope a plugin is installed in. The CLI enables
// and disables ids it has never seen, so every write on an installed plugin
// checks it against the list first.
func (r pluginRunner) installedScope(ctx context.Context, id string) (string, error) {
	plugins, err := r.installed(ctx)
	if err != nil {
		return "", err
	}
	for _, plugin := range plugins {
		if plugin.ID != id {
			continue
		}
		if !slices.Contains(pluginScopes, plugin.Scope) {
			return "", fmt.Errorf("%w: plugin %s is set by %s settings", bridgeipc.ErrConflict, id, plugin.Scope)
		}
		return plugin.Scope, nil
	}
	return "", fmt.Errorf("%w: plugin %s", bridgeipc.ErrNotFound, id)
}

func splitPluginID(id string) (name, marketplace string) {
	name, marketplace, _ = strings.CutLast(id, "@")
	return name, marketplace
}

func marketplaceSource(m cliMarketplace) string {
	for _, value := range []string{m.Repo, m.URL, m.Path} {
		if value != "" {
			return value
		}
	}
	return m.Source
}

// ListPlugins returns the plugins installed for the user and the marketplaces
// the agent knows.
func ListPlugins(ctx context.Context, user session.User, agent string) (apischema.AssistantPluginList, error) {
	runner, err := newPluginRunner(ctx, user, agent)
	if err != nil {
		return apischema.AssistantPluginList{}, err
	}
	installed, err := runner.installed(ctx)
	if err != nil {
		return apischema.AssistantPluginList{}, err
	}
	out, err := runner.run(ctx, pluginReadTimeout, "marketplace", "list", "--json")
	if err != nil {
		return apischema.AssistantPluginList{}, err
	}
	var marketplaces []cliMarketplace
	if err := json.Unmarshal(out, &marketplaces); err != nil {
		return apischema.AssistantPluginList{}, fmt.Errorf("decode claude plugin marketplace list: %w", err)
	}

	result := apischema.AssistantPluginList{
		Plugins:      make([]apischema.AssistantPlugin, 0, len(installed)),
		Marketplaces: make([]apischema.AssistantMarketplace, 0, len(marketplaces)),
	}
	for _, plugin := range installed {
		name, marketplace := splitPluginID(plugin.ID)
		result.Plugins = append(result.Plugins, apischema.AssistantPlugin{
			ID: plugin.ID, Name: name, Marketplace: marketplace,
			Version: plugin.Version, Scope: plugin.Scope, Enabled: plugin.Enabled,
		})
	}
	slices.SortFunc(result.Plugins, func(a, b apischema.AssistantPlugin) int { return cmp.Compare(a.ID, b.ID) })
	for _, marketplace := range marketplaces {
		result.Marketplaces = append(result.Marketplaces, apischema.AssistantMarketplace{
			Name: marketplace.Name, Source: marketplaceSource(marketplace),
		})
	}
	slices.SortFunc(result.Marketplaces, func(a, b apischema.AssistantMarketplace) int { return cmp.Compare(a.Name, b.Name) })
	return result, nil
}

// SearchPlugins returns the most installed catalogue entries that match query
// and are not installed yet.
func SearchPlugins(ctx context.Context, user session.User, agent, query string) (apischema.AssistantPluginSearchResult, error) {
	if len(query) > pluginValueMax {
		return apischema.AssistantPluginSearchResult{}, fmt.Errorf("%w: query", bridgeipc.ErrInvalidArgs)
	}
	runner, err := newPluginRunner(ctx, user, agent)
	if err != nil {
		return apischema.AssistantPluginSearchResult{}, err
	}
	out, err := runner.run(ctx, pluginReadTimeout, "list", "--json", "--available")
	if err != nil {
		return apischema.AssistantPluginSearchResult{}, err
	}
	var catalogue struct {
		Installed []cliInstalledPlugin `json:"installed"`
		Available []cliAvailablePlugin `json:"available"`
	}
	if err := json.Unmarshal(out, &catalogue); err != nil {
		return apischema.AssistantPluginSearchResult{}, fmt.Errorf("decode claude plugin list: %w", err)
	}

	installed := make(map[string]bool, len(catalogue.Installed))
	for _, plugin := range catalogue.Installed {
		installed[plugin.ID] = true
	}
	words := strings.Fields(strings.ToLower(query))
	matches := make([]apischema.AssistantAvailablePlugin, 0, pluginSearchLimit)
	for _, plugin := range catalogue.Available {
		if installed[plugin.PluginID] {
			continue
		}
		text := strings.ToLower(plugin.Name + "\n" + plugin.Description + "\n" + plugin.MarketplaceName)
		if slices.ContainsFunc(words, func(word string) bool { return !strings.Contains(text, word) }) {
			continue
		}
		matches = append(matches, apischema.AssistantAvailablePlugin{
			ID: plugin.PluginID, Name: plugin.Name, Description: plugin.Description,
			Marketplace: plugin.MarketplaceName, InstallCount: plugin.InstallCount,
		})
	}
	slices.SortFunc(matches, func(a, b apischema.AssistantAvailablePlugin) int {
		return cmp.Or(cmp.Compare(b.InstallCount, a.InstallCount), cmp.Compare(a.ID, b.ID))
	})
	return apischema.AssistantPluginSearchResult{
		Plugins: matches[:min(len(matches), pluginSearchLimit)],
		Total:   len(matches),
	}, nil
}

func validPluginID(id string) error {
	if len(id) > pluginValueMax || !pluginIDPattern.MatchString(id) {
		return fmt.Errorf("%w: plugin", bridgeipc.ErrInvalidArgs)
	}
	return nil
}

// InstallPlugin installs a plugin in the user's own settings scope. A plugin
// whose marketplace declares an install command is not installed until the
// caller repeats the request with that command's sha256: the result carries
// the command for the user to read.
func InstallPlugin(ctx context.Context, user session.User, req apischema.AssistantPluginInstallRequest) (apischema.AssistantPluginInstallResult, error) {
	if err := validPluginID(req.Plugin); err != nil {
		return apischema.AssistantPluginInstallResult{}, err
	}
	if req.AcceptCommand != "" && !commandSHAPattern.MatchString(req.AcceptCommand) {
		return apischema.AssistantPluginInstallResult{}, fmt.Errorf("%w: accepted command", bridgeipc.ErrInvalidArgs)
	}
	runner, err := newPluginRunner(ctx, user, req.Agent)
	if err != nil {
		return apischema.AssistantPluginInstallResult{}, err
	}
	args := []string{"install", req.Plugin, "--json", "--scope", "user"}
	if req.AcceptCommand != "" {
		args = append(args, "--accept-command", req.AcceptCommand)
	}
	if _, err = runner.run(ctx, pluginWriteTimeout, args...); err != nil {
		var cliErr *pluginCLIError
		if errors.As(err, &cliErr) && cliErr.outcome != nil && cliErr.outcome.ShownCommand != nil {
			shown := cliErr.outcome.ShownCommand
			if shown.Command != "" && commandSHAPattern.MatchString(shown.SHA256) && shown.SHA256 != req.AcceptCommand {
				return apischema.AssistantPluginInstallResult{
					Command: &apischema.AssistantPluginCommand{Command: shown.Command, SHA256: shown.SHA256},
				}, nil
			}
		}
		return apischema.AssistantPluginInstallResult{}, err
	}
	slog.Info("installed assistant plugin", "component", "assistant", "agent", req.Agent, "plugin", req.Plugin, "user", user.Username)
	return apischema.AssistantPluginInstallResult{Installed: true}, nil
}

// UninstallPlugin removes an installed plugin from the scope it is in.
func UninstallPlugin(ctx context.Context, user session.User, req apischema.AssistantPluginRequest) error {
	if err := validPluginID(req.Plugin); err != nil {
		return err
	}
	runner, err := newPluginRunner(ctx, user, req.Agent)
	if err != nil {
		return err
	}
	scope, err := runner.installedScope(ctx, req.Plugin)
	if err != nil {
		return err
	}
	if _, err = runner.run(ctx, pluginWriteTimeout, "uninstall", req.Plugin, "--json", "--scope", scope); err != nil {
		return err
	}
	slog.Info("uninstalled assistant plugin", "component", "assistant", "agent", req.Agent, "plugin", req.Plugin, "user", user.Username)
	return nil
}

// SetPluginEnabled turns an installed plugin on or off in the scope it is in.
func SetPluginEnabled(ctx context.Context, user session.User, req apischema.AssistantPluginEnabledRequest) error {
	if err := validPluginID(req.Plugin); err != nil {
		return err
	}
	runner, err := newPluginRunner(ctx, user, req.Agent)
	if err != nil {
		return err
	}
	scope, err := runner.installedScope(ctx, req.Plugin)
	if err != nil {
		return err
	}
	verb := "disable"
	if req.Enabled {
		verb = "enable"
	}
	if _, err = runner.run(ctx, pluginWriteTimeout, verb, req.Plugin, "--json", "--scope", scope); err != nil {
		return err
	}
	slog.Info("changed assistant plugin state", "component", "assistant", "agent", req.Agent, "plugin", req.Plugin, "enabled", req.Enabled, "user", user.Username)
	return nil
}

// validMarketplaceSource accepts a GitHub "owner/repo" or an https URL. Local
// paths and other schemes are left to the user's own terminal.
func validMarketplaceSource(source string) error {
	if len(source) > pluginValueMax {
		return fmt.Errorf("%w: marketplace source", bridgeipc.ErrInvalidArgs)
	}
	if githubRepoPattern.MatchString(source) {
		return nil
	}
	parsed, err := url.Parse(source)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("%w: marketplace source", bridgeipc.ErrInvalidArgs)
	}
	return nil
}

// loggedMarketplaceSource is a source without the query or fragment of its
// URL, where a private catalogue may carry a token.
func loggedMarketplaceSource(source string) string {
	parsed, err := url.Parse(source)
	if err != nil || parsed.Scheme == "" {
		return source
	}
	parsed.RawQuery, parsed.Fragment = "", ""
	return parsed.String()
}

func validMarketplaceName(name string) error {
	if len(name) > pluginValueMax || !marketplaceNamePattern.MatchString(name) {
		return fmt.Errorf("%w: marketplace", bridgeipc.ErrInvalidArgs)
	}
	return nil
}

// AddMarketplace adds a plugin catalogue to the user's own settings.
func AddMarketplace(ctx context.Context, user session.User, req apischema.AssistantMarketplaceAddRequest) error {
	if err := validMarketplaceSource(req.Source); err != nil {
		return err
	}
	runner, err := newPluginRunner(ctx, user, req.Agent)
	if err != nil {
		return err
	}
	if _, err = runner.run(ctx, pluginWriteTimeout, "marketplace", "add", req.Source, "--json", "--scope", "user"); err != nil {
		return err
	}
	slog.Info("added assistant plugin marketplace", "component", "assistant", "agent", req.Agent, "source", loggedMarketplaceSource(req.Source), "user", user.Username)
	return nil
}

// RemoveMarketplace removes a plugin catalogue from every settings scope.
func RemoveMarketplace(ctx context.Context, user session.User, req apischema.AssistantMarketplaceRequest) error {
	if err := validMarketplaceName(req.Name); err != nil {
		return err
	}
	runner, err := newPluginRunner(ctx, user, req.Agent)
	if err != nil {
		return err
	}
	if _, err = runner.run(ctx, pluginWriteTimeout, "marketplace", "remove", req.Name, "--json"); err != nil {
		return err
	}
	slog.Info("removed assistant plugin marketplace", "component", "assistant", "agent", req.Agent, "marketplace", req.Name, "user", user.Username)
	return nil
}

// UpdateMarketplace refreshes one plugin catalogue from its source.
func UpdateMarketplace(ctx context.Context, user session.User, req apischema.AssistantMarketplaceRequest) error {
	if err := validMarketplaceName(req.Name); err != nil {
		return err
	}
	runner, err := newPluginRunner(ctx, user, req.Agent)
	if err != nil {
		return err
	}
	if _, err = runner.run(ctx, pluginWriteTimeout, "marketplace", "update", req.Name, "--json"); err != nil {
		return err
	}
	slog.Info("updated assistant plugin marketplace", "component", "assistant", "agent", req.Agent, "marketplace", req.Name, "user", user.Username)
	return nil
}
