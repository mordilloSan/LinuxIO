import { useQuery } from "@tanstack/react-query";
import { type ReactNode, useState } from "react";

import {
  type AssistantMarketplace,
  type AssistantPlugin,
  type AssistantPluginCommand,
  linuxio,
} from "@/api";
import type { AssistantAgentId } from "@/api/acp/agents";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import TabSelector from "@/components/tabbar/TabSelector";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import ConfirmDialog from "@/components/ui/AppConfirmDialog";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppSearchField from "@/components/ui/AppSearchField";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { useDebouncedValue } from "@/hooks/useDebouncedValue";

import "./plugins-dialog.css";

type Tab = "installed" | "browse" | "marketplaces";

const TABS: ReadonlyArray<{ label: string; value: Tab }> = [
  { label: "Installed", value: "installed" },
  { label: "Browse", value: "browse" },
  { label: "Marketplaces", value: "marketplaces" },
];

// Scopes a user's own settings hold; anything else is set by an organization.
const USER_SCOPES = ["user", "project", "local"];

const SEARCH_DEBOUNCE_MS = 300;

const installs = new Intl.NumberFormat();

interface CommandPrompt {
  plugin: string;
  command: AssistantPluginCommand;
}

function Row({
  title,
  detail,
  children,
  actions,
}: {
  title: string;
  detail: string;
  children?: ReactNode;
  actions: ReactNode;
}) {
  return (
    <li className="assistant-plugins__row">
      <div className="assistant-plugins__row-text">
        <AppTypography fontWeight={600} noWrap>
          {title}
        </AppTypography>
        {children}
        <AppTypography color="text.secondary" noWrap variant="caption">
          {detail}
        </AppTypography>
      </div>
      <div className="assistant-plugins__row-actions">{actions}</div>
    </li>
  );
}

function Notice({ children }: { children: ReactNode }) {
  return (
    <AppTypography
      className="assistant-plugins__notice"
      color="text.secondary"
      role="status"
    >
      {children}
    </AppTypography>
  );
}

function LoadError({ error, onRetry }: { error: Error; onRetry: () => void }) {
  return (
    <AppAlert
      action={<AppButton onClick={onRetry}>Retry</AppButton>}
      severity="error"
    >
      {error.message}
    </AppAlert>
  );
}

function InstalledTab({
  agent,
  plugins,
  onChanged,
}: {
  agent: AssistantAgentId;
  plugins: AssistantPlugin[];
  onChanged: () => void;
}) {
  const [removing, setRemoving] = useState<AssistantPlugin | null>(null);
  const setEnabled = linuxio.assistant.set_plugin_enabled.useMutation({
    success: onChanged,
    error: "Could not change the plugin",
  });
  const uninstall = linuxio.assistant.uninstall_plugin.useMutation({
    success: () => {
      setRemoving(null);
      onChanged();
    },
    error: "Could not uninstall the plugin",
  });

  if (plugins.length === 0) {
    return <Notice>No plugins are installed. Browse to add one.</Notice>;
  }
  return (
    <>
      <ul className="assistant-plugins__list">
        {plugins.map((plugin) => {
          const own = USER_SCOPES.includes(plugin.scope);
          const busy =
            setEnabled.isPending && setEnabled.variables?.plugin === plugin.id;
          return (
            <Row
              actions={
                <>
                  <AppSwitch
                    aria-label={`Enable ${plugin.name}`}
                    checked={plugin.enabled}
                    disabled={!own || busy}
                    onChange={(_, enabled) =>
                      setEnabled.mutate({ agent, plugin: plugin.id, enabled })
                    }
                    size="small"
                  />
                  <AppActionIconButton
                    ariaLabel={`Uninstall ${plugin.name}`}
                    disabled={!own}
                    icon="mdi:trash-can-outline"
                    label="Uninstall"
                    onClick={() => setRemoving(plugin)}
                  />
                </>
              }
              detail={
                own
                  ? `${plugin.marketplace} · ${plugin.version}`
                  : `${plugin.marketplace} · ${plugin.version} · set by ${plugin.scope} settings`
              }
              key={plugin.id}
              title={plugin.name}
            />
          );
        })}
      </ul>
      <ConfirmDialog
        confirmText="Uninstall"
        destructive
        isPending={uninstall.isPending}
        message={`${removing?.name ?? "This plugin"} and the data it saved will be removed.`}
        onClose={() => setRemoving(null)}
        onConfirm={() => {
          if (removing) uninstall.mutate({ agent, plugin: removing.id });
        }}
        open={removing !== null}
        pendingText="Uninstalling…"
        title={`Uninstall ${removing?.name ?? "plugin"}?`}
      />
    </>
  );
}

function BrowseTab({
  agent,
  onChanged,
}: {
  agent: AssistantAgentId;
  onChanged: () => void;
}) {
  const [query, setQuery] = useState("");
  const [prompt, setPrompt] = useState<CommandPrompt | null>(null);
  const settled = useDebouncedValue(query.trim(), SEARCH_DEBOUNCE_MS);
  const search = useQuery(
    linuxio.assistant.search_plugins({ agent, query: settled }),
  );
  const install = linuxio.assistant.install_plugin.useMutation({
    success: (result, variables) => {
      // A marketplace that installs by running a command is shown that
      // command first; nothing has run yet.
      if (result.command) {
        setPrompt({ plugin: variables.plugin, command: result.command });
        return;
      }
      setPrompt(null);
      onChanged();
    },
    error: "Could not install the plugin",
  });
  const result = search.data;

  return (
    <>
      <AppSearchField
        aria-label="Search plugins"
        autoFocus
        fullWidth
        onChange={(event) => setQuery(event.target.value)}
        placeholder="Search plugins"
        value={query}
      />
      {search.error ? (
        <LoadError error={search.error} onRetry={() => void search.refetch()} />
      ) : !result ? (
        <Notice>Loading plugins…</Notice>
      ) : result.plugins.length === 0 ? (
        <Notice>
          {settled
            ? `No plugin matches "${settled}".`
            : "Every plugin in your marketplaces is installed."}
        </Notice>
      ) : (
        <>
          <ul className="assistant-plugins__list">
            {result.plugins.map((plugin) => {
              const busy =
                install.isPending && install.variables?.plugin === plugin.id;
              return (
                <Row
                  actions={
                    <AppButton
                      aria-label={`Install ${plugin.name}`}
                      disabled={install.isPending}
                      onClick={() =>
                        install.mutate({ agent, plugin: plugin.id })
                      }
                      size="small"
                      variant="outlined"
                    >
                      {busy ? "Installing…" : "Install"}
                    </AppButton>
                  }
                  detail={`${plugin.marketplace} · ${installs.format(plugin.installCount)} installs`}
                  key={plugin.id}
                  title={plugin.name}
                >
                  {plugin.description ? (
                    <AppTypography
                      className="assistant-plugins__description"
                      variant="body2"
                    >
                      {plugin.description}
                    </AppTypography>
                  ) : null}
                </Row>
              );
            })}
          </ul>
          {result.total > result.plugins.length ? (
            <Notice>
              Showing the {result.plugins.length} most installed of{" "}
              {installs.format(result.total)}. Search to narrow the list.
            </Notice>
          ) : null}
        </>
      )}
      <GeneralDialog
        aria-label="Install command"
        fullWidth
        maxWidth="sm"
        onClose={() => setPrompt(null)}
        open={prompt !== null}
      >
        <AppDialogTitle>Run this command to install?</AppDialogTitle>
        <AppDialogContent>
          <AppDialogContentText>
            The marketplace installs {prompt?.plugin} by running this command on
            this server as you. Nothing has run yet.
          </AppDialogContentText>
          <pre className="assistant-plugins__command">
            {prompt?.command.command}
          </pre>
        </AppDialogContent>
        <AppDialogActions>
          <AppButton
            disabled={install.isPending}
            onClick={() => setPrompt(null)}
          >
            Cancel
          </AppButton>
          <AppButton
            color="warning"
            disabled={install.isPending}
            onClick={() => {
              if (!prompt) return;
              install.mutate({
                agent,
                plugin: prompt.plugin,
                acceptCommand: prompt.command.sha256,
              });
            }}
            variant="contained"
          >
            {install.isPending ? "Installing…" : "Run and install"}
          </AppButton>
        </AppDialogActions>
      </GeneralDialog>
    </>
  );
}

function MarketplacesTab({
  agent,
  marketplaces,
}: {
  agent: AssistantAgentId;
  marketplaces: AssistantMarketplace[];
}) {
  const [source, setSource] = useState("");
  const [removing, setRemoving] = useState<AssistantMarketplace | null>(null);
  const add = linuxio.assistant.add_marketplace.useMutation({
    success: () => setSource(""),
    error: "Could not add the marketplace",
  });
  const update = linuxio.assistant.update_marketplace.useMutation({
    success: "Marketplace refreshed",
    error: "Could not refresh the marketplace",
  });
  const remove = linuxio.assistant.remove_marketplace.useMutation({
    success: () => setRemoving(null),
    error: "Could not remove the marketplace",
  });
  const trimmed = source.trim();

  return (
    <>
      <form
        className="assistant-plugins__add"
        onSubmit={(event) => {
          event.preventDefault();
          if (trimmed && !add.isPending) add.mutate({ agent, source: trimmed });
        }}
      >
        <AppTextField
          aria-label="Marketplace source"
          fullWidth
          helperText="A GitHub repository (owner/repo) or an https URL."
          onChange={(event) => setSource(event.target.value)}
          placeholder="owner/repo"
          size="small"
          value={source}
        />
        <AppButton
          disabled={!trimmed || add.isPending}
          type="submit"
          variant="outlined"
        >
          {add.isPending ? "Adding…" : "Add"}
        </AppButton>
      </form>
      {marketplaces.length === 0 ? (
        <Notice>No marketplaces yet. Add one to browse its plugins.</Notice>
      ) : (
        <ul className="assistant-plugins__list">
          {marketplaces.map((marketplace) => (
            <Row
              actions={
                <>
                  <AppActionIconButton
                    ariaLabel={`Refresh ${marketplace.name}`}
                    icon="mdi:refresh"
                    label="Refresh"
                    loading={
                      update.isPending &&
                      update.variables?.name === marketplace.name
                    }
                    onClick={() =>
                      update.mutate({ agent, name: marketplace.name })
                    }
                  />
                  <AppActionIconButton
                    ariaLabel={`Remove ${marketplace.name}`}
                    icon="mdi:trash-can-outline"
                    label="Remove"
                    onClick={() => setRemoving(marketplace)}
                  />
                </>
              }
              detail={marketplace.source}
              key={marketplace.name}
              title={marketplace.name}
            />
          ))}
        </ul>
      )}
      <ConfirmDialog
        confirmText="Remove"
        destructive
        isPending={remove.isPending}
        message={`Plugins from ${removing?.name ?? "this marketplace"} will no longer be listed under Browse.`}
        onClose={() => setRemoving(null)}
        onConfirm={() => {
          if (removing) remove.mutate({ agent, name: removing.name });
        }}
        open={removing !== null}
        pendingText="Removing…"
        title={`Remove ${removing?.name ?? "marketplace"}?`}
      />
    </>
  );
}

/**
 * Installs, enables and removes the agent's plugins, the way the agent's own
 * editors do it. The running chat keeps the plugins it started with, so a
 * change is offered a new chat.
 */
export default function PluginsDialog({
  agent,
  open,
  onClose,
  onNewChat,
}: {
  agent: AssistantAgentId;
  open: boolean;
  onClose: () => void;
  /** Starts a new chat; absent while the running turn cannot be replaced. */
  onNewChat?: () => void;
}) {
  const [tab, setTab] = useState<Tab>("installed");
  const [changed, setChanged] = useState(false);
  const list = useQuery({
    ...linuxio.assistant.list_plugins({ agent }),
    enabled: open,
  });
  const markChanged = () => setChanged(true);

  return (
    <GeneralDialog
      aria-label="Plugins"
      fullWidth
      maxWidth="md"
      onClose={onClose}
      open={open}
      slotProps={{ transition: { onExited: () => setChanged(false) } }}
    >
      <AppDialogTitle>Plugins</AppDialogTitle>
      <AppDialogContent className="assistant-plugins">
        <TabSelector onChange={setTab} options={TABS} value={tab} />
        {changed ? (
          <AppAlert
            action={
              onNewChat ? (
                <AppButton
                  onClick={() => {
                    onNewChat();
                    onClose();
                  }}
                >
                  New chat
                </AppButton>
              ) : undefined
            }
            severity="info"
          >
            This chat keeps the plugins it started with. Changes apply to new
            chats.
          </AppAlert>
        ) : null}
        <div className="assistant-plugins__body">
          {tab === "browse" ? (
            <BrowseTab agent={agent} onChanged={markChanged} />
          ) : list.error ? (
            <LoadError error={list.error} onRetry={() => void list.refetch()} />
          ) : !list.data ? (
            <Notice>Loading plugins…</Notice>
          ) : tab === "installed" ? (
            <InstalledTab
              agent={agent}
              onChanged={markChanged}
              plugins={list.data.plugins}
            />
          ) : (
            <MarketplacesTab
              agent={agent}
              marketplaces={list.data.marketplaces}
            />
          )}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={onClose}>Close</AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
}
