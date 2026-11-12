import { Icon } from "@iconify/react";
import { useNavigate } from "@tanstack/react-router";
import { type CSSProperties, useEffect, useState } from "react";

import { type AssistantChatDefaults, useStreamMux } from "@/api";
import { ASSISTANT_AGENTS, type AssistantAgentId } from "@/api/acp/agents";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import ConfirmDialog from "@/components/ui/AppConfirmDialog";
import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";
import AppTypography from "@/components/ui/AppTypography";
import HeaderActions from "@/components/ui/HeaderActions";
import { OVERLAY_ROOT_SELECTOR } from "@/components/ui/overlay";
import { useAssistantRequirements } from "@/hooks/useAssistantRequirements";
import { useConfigValue } from "@/hooks/useConfig";
import {
  readPersistedState,
  writePersistedState,
} from "@/utils/persistedState";

import { assistantStore, chatPane, useAssistantState } from "./assistant-store";
import Composer from "./composer/Composer";
import PanePanel from "./pane/PanePanel";
import { paneTitle } from "./pane/readPaneTree";
import PluginsDialog from "./plugins/PluginsDialog";
import SidePanel, { SidePanelOpening } from "./session/SidePanel";
import StatusLine from "./session/StatusLine";
import PermissionCard from "./transcript/PermissionCard";
import Transcript from "./transcript/Transcript";

import "./assistant-tokens.css";
import "./assistant.css";

// Escape inside an overlay belongs to that overlay, not to the running turn.
const OVERLAY_SELECTOR = `${OVERLAY_ROOT_SELECTOR}, .app-popover-root, [role="menu"], [role="listbox"]`;

// Same bounds the terminal enforces for terminalFontSize.
const MIN_FONT = 10;
const MAX_FONT = 28;
const FONT_SIZE_KEY = "linuxio.assistant.fontSize";

const isFontSize = (value: unknown): value is number =>
  typeof value === "number" && value >= MIN_FONT && value <= MAX_FONT;

export default function AssistantPage({
  agent,
  chatDefaults,
}: {
  agent: AssistantAgentId | null;
  chatDefaults?: AssistantChatDefaults;
}) {
  const { isOpen } = useStreamMux();
  const state = useAssistantState();
  const navigate = useNavigate();
  const agentEntry = ASSISTANT_AGENTS.find((entry) => entry.id === agent);
  const agentLabel = agentEntry?.label ?? agent;
  const [pluginsOpen, setPluginsOpen] = useState(false);
  const [historyAnchor, setHistoryAnchor] = useState<HTMLElement | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<{
    sessionId: string;
    label: string;
  } | null>(null);
  // The plugin pane the user closed; a header button brings it back.
  const [hiddenPaneId, setHiddenPaneId] = useState<string | null>(null);

  // Follows the terminal's font size until the user picks one here.
  const [terminalFontSize] = useConfigValue("terminalFontSize");
  const [ownFontSize, setOwnFontSize] = useState(() =>
    readPersistedState(FONT_SIZE_KEY, isFontSize),
  );
  const fontSize = ownFontSize ?? terminalFontSize;
  const adjustFontSize = (delta: number) => {
    const next = Math.min(MAX_FONT, Math.max(MIN_FONT, fontSize + delta));
    setOwnFontSize(next);
    writePersistedState(FONT_SIZE_KEY, next);
  };

  // Ahead of connect, so the first chat already starts on them.
  useEffect(() => {
    assistantStore.setChatDefaults(chatDefaults);
  }, [chatDefaults]);

  // Starting an agent never installs anything, so it is only started once
  // everything it needs is installed.
  const { ready, missing } = useAssistantRequirements(agent);
  useEffect(() => {
    if (agent && isOpen && ready) void assistantStore.connect(agent);
  }, [agent, isOpen, ready]);

  const { status } = state;
  const sideRunning = state.side?.status === "running";
  const sideShown = state.side?.open || (state.sideOpening && !state.side);
  const pane = chatPane(state);
  const paneHidden = pane !== null && pane.id === hiddenPaneId;
  // A side question takes the space beside the chat while it is open.
  const shownPane = pane && !paneHidden && !sideShown ? pane : null;
  const loadingOrRunning = status === "running" || status === "connecting";
  // Page shortcuts work wherever focus is, e.g. on <body> after the Stop button
  // took it away.
  useEffect(() => {
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      if (
        (event.ctrlKey || event.metaKey) &&
        event.shiftKey &&
        event.key.toLowerCase() === "o"
      ) {
        event.preventDefault();
        if (!loadingOrRunning) void assistantStore.newSession();
        return;
      }
      // Escape inside the side panel belongs to the side chat, never the main turn.
      if (
        event.key === "Escape" &&
        event.target instanceof Element &&
        event.target.closest(".assistant__side")
      ) {
        if (sideRunning && !event.defaultPrevented) assistantStore.stopSide();
        return;
      }
      if (
        event.key === "Escape" &&
        status === "running" &&
        !event.defaultPrevented &&
        !(
          event.target instanceof Element &&
          event.target.closest(OVERLAY_SELECTOR)
        )
      ) {
        assistantStore.stop();
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [status, loadingOrRunning, sideRunning]);

  if (!agent) {
    return (
      <AppAlert
        severity="info"
        action={
          <AppButton
            onClick={() =>
              void navigate({ to: "/settings", search: { tab: "assistant" } })
            }
          >
            Choose an agent
          </AppButton>
        }
      >
        No agent is set up yet. Choose one to start a conversation.
      </AppAlert>
    );
  }

  if (missing.length > 0) {
    return (
      <AppAlert
        severity="warning"
        action={
          <AppButton
            onClick={() =>
              void navigate({
                to: "/settings",
                search: { tab: "capabilities" },
              })
            }
          >
            Open Capabilities
          </AppButton>
        }
      >
        The agent cannot start until these are installed.
        {missing.map((item) => (
          <div key={item.key}>
            <strong>{item.label}:</strong> {item.reason}
          </div>
        ))}
      </AppAlert>
    );
  }

  const actions = (
    <HeaderActions
      options={
        <>
          <AppActionIconButton
            disabled={fontSize <= MIN_FONT}
            icon="mdi:format-font-size-decrease"
            iconSize={20}
            label="Decrease font size"
            onClick={() => adjustFontSize(-1)}
          />
          <AppTypography
            align="center"
            color="text.secondary"
            style={{ minWidth: 24, fontVariantNumeric: "tabular-nums" }}
            variant="body2"
          >
            {fontSize}
          </AppTypography>
          <AppActionIconButton
            disabled={fontSize >= MAX_FONT}
            icon="mdi:format-font-size-increase"
            iconSize={20}
            label="Increase font size"
            onClick={() => adjustFontSize(1)}
          />
          <AppActionIconButton
            ariaLabel="Previous chats"
            disabled={!state.canListSessions}
            icon="mdi:history"
            iconSize={20}
            label="Previous chats"
            onClick={(event) => setHistoryAnchor(event.currentTarget)}
          />
          {/* Also here, not only as /plugins: a plugin that stops the agent
              from starting leaves the composer disabled. */}
          {agentEntry?.plugins ? (
            <AppActionIconButton
              ariaLabel="Plugins"
              icon="mdi:puzzle-outline"
              iconSize={20}
              label="Plugins"
              onClick={() => setPluginsOpen(true)}
            />
          ) : null}
          {paneHidden ? (
            <AppActionIconButton
              ariaLabel={`Show ${paneTitle(pane)}`}
              icon="mdi:dock-right"
              iconSize={20}
              label={`Show ${paneTitle(pane)}`}
              onClick={() => setHiddenPaneId(null)}
            />
          ) : null}
        </>
      }
      create={
        <AppActionIconButton
          ariaLabel="New chat"
          disabled={loadingOrRunning}
          icon="mdi:chat-plus-outline"
          iconSize={20}
          label="New chat (Ctrl+Shift+O)"
          onClick={() => void assistantStore.newSession()}
        />
      }
    />
  );

  return (
    <div
      className="assistant"
      style={{ "--app-font-size-body": `${fontSize}px` } as CSSProperties}
    >
      {/* Like the terminal: the chat's icons sit in the page above it, not
          in the shell header. */}
      <div className="assistant__actions">{actions}</div>
      <AppMenu
        anchorEl={historyAnchor}
        ariaLabel="Previous chats"
        minWidth={240}
        onClose={() => setHistoryAnchor(null)}
        open={historyAnchor !== null}
      >
        {state.sessions.length === 0 ? (
          <AppMenuItem disabled>No previous chats</AppMenuItem>
        ) : (
          state.sessions.map((session) => {
            const label = session.title ?? session.sessionId;
            const isOpenChat = session.sessionId === state.sessionId;
            return (
              <div className="assistant__history-row" key={session.sessionId}>
                <AppMenuItem
                  disabled={!state.canLoadSession || loadingOrRunning}
                  onClick={() => {
                    setHistoryAnchor(null);
                    void assistantStore.selectSession(session.sessionId);
                  }}
                  selected={isOpenChat}
                >
                  {label}
                </AppMenuItem>
                {state.canDeleteSession ? (
                  <AppActionIconButton
                    ariaLabel={`Delete ${label}`}
                    disabled={isOpenChat && loadingOrRunning}
                    icon="mdi:delete-outline"
                    onClick={() => {
                      setHistoryAnchor(null);
                      setDeleteTarget({
                        sessionId: session.sessionId,
                        label,
                      });
                    }}
                    size="small"
                    tooltip={false}
                  />
                ) : null}
              </div>
            );
          })
        )}
      </AppMenu>
      <ConfirmDialog
        confirmText="Delete chat"
        destructive
        message={`Delete "${deleteTarget?.label ?? ""}"? This cannot be undone.`}
        onClose={() => setDeleteTarget(null)}
        onConfirm={() => {
          if (deleteTarget)
            void assistantStore.deleteSession(deleteTarget.sessionId);
          setDeleteTarget(null);
        }}
        open={deleteTarget !== null}
        title="Delete chat"
      />
      {agent && agentEntry?.plugins ? (
        <PluginsDialog
          agent={agent}
          // A running turn cannot be replaced by a new chat.
          onNewChat={
            loadingOrRunning
              ? undefined
              : () => void assistantStore.newSession()
          }
          onClose={() => setPluginsOpen(false)}
          open={pluginsOpen}
        />
      ) : null}

      <div
        className={`assistant__body${sideShown ? " assistant__body--split" : ""}`}
      >
        <section className="assistant__main">
          {!ready ? (
            <AppTypography color="text.secondary">
              Checking what is installed…
            </AppTypography>
          ) : null}
          {state.status === "connecting" ? (
            <AppTypography color="text.secondary">
              Starting {agentLabel}…
            </AppTypography>
          ) : null}
          {state.status === "auth_required" ? (
            <AppAlert severity="warning">
              <div>The agent needs you to log in.</div>
              <div className="assistant__composer">
                {state.authMethods.map((method) =>
                  "type" in method && method.type === "terminal" ? (
                    <AppButton
                      key={method.id}
                      onClick={() =>
                        void navigate({
                          to: "/terminal",
                          state: {
                            terminalLogin: {
                              agent,
                              args: [...(method.args ?? [])],
                            },
                          },
                        })
                      }
                      variant="outlined"
                    >
                      {method.name}
                    </AppButton>
                  ) : null,
                )}
                <AppButton onClick={() => void assistantStore.restart()}>
                  Reconnect
                </AppButton>
              </div>
            </AppAlert>
          ) : null}
          {state.status === "stopped" ? (
            <AppAlert
              severity="error"
              action={
                <AppButton onClick={() => void assistantStore.restart()}>
                  Restart
                </AppButton>
              }
            >
              <div>
                Agent stopped
                {state.exit ? ` (exit code ${state.exit.code})` : ""}.
              </div>
              {state.exit?.stderr ? <pre>{state.exit.stderr}</pre> : null}
            </AppAlert>
          ) : null}
          {state.error ? (
            <AppAlert
              severity="error"
              action={
                state.status === "error" ? (
                  <AppButton onClick={() => void assistantStore.restart()}>
                    Restart
                  </AppButton>
                ) : undefined
              }
            >
              {state.error}
            </AppAlert>
          ) : null}

          <Transcript
            blocks={state.blocks}
            pendingToolCallId={
              state.pending?.request.toolCall.toolCallId ?? null
            }
            sessionId={state.sessionId}
          >
            {state.status === "ready" ? (
              <div className="assistant__empty">
                <Icon icon="mdi:robot-outline" width={48} />
                <AppTypography>New chat</AppTypography>
                <AppTypography color="text.secondary">
                  Ask {agentLabel} anything about this host.
                </AppTypography>
              </div>
            ) : null}
          </Transcript>

          {state.pending ? (
            <PermissionCard
              key={state.pending.request.toolCall.toolCallId}
              request={state.pending.request}
              onAnswer={(optionId) => assistantStore.answerPermission(optionId)}
            />
          ) : null}

          {status === "running" ? (
            <StatusLine turnStartedAt={state.turnStartedAt} />
          ) : null}
          <Composer
            canEmbed={state.promptCapabilities.embeddedContext}
            canFork={state.canFork}
            canImages={state.promptCapabilities.image}
            canSteer={state.canSteer}
            commands={state.commands}
            configOptions={state.configOptions}
            connecting={status === "connecting"}
            modes={state.modes}
            disabled={state.status !== "ready" && state.status !== "running"}
            onBtw={(text) => void assistantStore.openSide(text || undefined)}
            onPlugins={
              agentEntry?.plugins ? () => setPluginsOpen(true) : undefined
            }
            onSend={(blocks, display) =>
              void assistantStore.send(blocks, display)
            }
            onSteer={(blocks, display) =>
              void assistantStore.steer(blocks, display)
            }
            onSetConfigOption={(configId, value) =>
              void assistantStore.setConfigOption(configId, value)
            }
            onSetMode={(modeId) => void assistantStore.setMode(modeId)}
            onStop={() => assistantStore.stop()}
            running={state.status === "running"}
            usage={state.usage}
          />
        </section>
        {state.side?.open ? (
          <SidePanel side={state.side} />
        ) : state.sideOpening && !state.side ? (
          <SidePanelOpening />
        ) : shownPane ? (
          <PanePanel
            onHide={() => setHiddenPaneId(shownPane.id)}
            pane={shownPane}
          />
        ) : null}
      </div>
    </div>
  );
}
