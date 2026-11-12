package apischema

type AssistantOpenRequest struct {
	Agent string `json:"agent"`
}

// AssistantAgentRequest names the agent whose plugins a request is about.
type AssistantAgentRequest struct {
	Agent string `json:"agent"`
}

// AssistantPlugin is one plugin installed for the session user.
type AssistantPlugin struct {
	// ID is the agent's own identifier, "<name>@<marketplace>".
	ID          string `json:"id"`
	Name        string `json:"name"`
	Marketplace string `json:"marketplace"`
	Version     string `json:"version"`
	// Scope is the settings scope the agent installed the plugin in.
	Scope   string `json:"scope"`
	Enabled bool   `json:"enabled"`
}

// AssistantMarketplace is one plugin catalogue the agent knows.
type AssistantMarketplace struct {
	Name string `json:"name"`
	// Source is where the catalogue comes from, as text to show: a GitHub
	// repository, a URL or a path.
	Source string `json:"source"`
}

type AssistantPluginList struct {
	Plugins      []AssistantPlugin      `json:"plugins"`
	Marketplaces []AssistantMarketplace `json:"marketplaces"`
}

type AssistantPluginSearchRequest struct {
	Agent string `json:"agent"`
	// Query matches plugin names, descriptions and marketplaces; empty lists
	// the most installed plugins.
	Query string `json:"query"`
}

// AssistantAvailablePlugin is one catalogue entry that is not installed.
type AssistantAvailablePlugin struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Marketplace  string `json:"marketplace"`
	InstallCount int64  `json:"installCount"`
}

type AssistantPluginSearchResult struct {
	Plugins []AssistantAvailablePlugin `json:"plugins"`
	// Total counts every match; Plugins holds the most installed of them.
	Total int `json:"total"`
}

type AssistantPluginInstallRequest struct {
	Agent  string `json:"agent"`
	Plugin string `json:"plugin"`
	// AcceptCommand is the sha256 of the marketplace-declared command a
	// previous install reported and the user then confirmed.
	AcceptCommand string `json:"acceptCommand,omitempty"`
}

// AssistantPluginCommand is a command a marketplace declares for installing
// one of its plugins. The agent runs it only once the user accepted exactly
// this command.
type AssistantPluginCommand struct {
	Command string `json:"command"`
	SHA256  string `json:"sha256"`
}

type AssistantPluginInstallResult struct {
	Installed bool `json:"installed"`
	// Command is set, with Installed false, when the install needs the user
	// to accept it first.
	Command *AssistantPluginCommand `json:"command,omitempty"`
}

type AssistantPluginRequest struct {
	Agent  string `json:"agent"`
	Plugin string `json:"plugin"`
}

type AssistantPluginEnabledRequest struct {
	Agent   string `json:"agent"`
	Plugin  string `json:"plugin"`
	Enabled bool   `json:"enabled"`
}

type AssistantMarketplaceAddRequest struct {
	Agent string `json:"agent"`
	// Source is a GitHub "owner/repo" or an https URL.
	Source string `json:"source"`
}

type AssistantMarketplaceRequest struct {
	Agent string `json:"agent"`
	Name  string `json:"name"`
}
