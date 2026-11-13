import { useNavigate } from "@tanstack/react-router";
import { type CSSProperties, useEffect, useState } from "react";

import { type AssistantChatDefaults, useStreamMux } from "@/api";
import { ASSISTANT_AGENTS, type AssistantAgentId } from "@/api/acp/agents";
import WidgetLoader from "@/components/loaders/WidgetLoader";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import ConfirmDialog from "@/components/ui/AppConfirmDialog";
import AppTypography from "@/components/ui/AppTypography";
import HeaderActions from "@/components/ui/HeaderActions";
import { OVERLAY_ROOT_SELECTOR } from "@/components/ui/overlay";
import { useAssistantRequirements } from "@/hooks/useAssistantRequirements";
import { useConfigValue } from "@/hooks/useConfig";
import { useAlerts } from "@/routes/_authenticated/-components/footer/useAlerts";
import {
  readPersistedState,
  writePersistedState,
} from "@/utils/persistedState";

import {
  assistantStore,
  chatPane,
  modelName,
  useAssistantState,
} from "./assistant-store";
import { buildPrompt } from "./attachments";
import Composer from "./composer/Composer";
import AssistantDock from "./dock/AssistantDock";
import PluginsDialog from "./plugins/PluginsDialog";
import SidePanel, { SidePanelOpening } from "./session/SidePanel";
import StatusLine from "./session/StatusLine";
import { OpenInTerminalContext } from "./transcript/MarkdownBlock";
import PermissionCard from "./transcript/PermissionCard";
import Transcript from "./transcript/Transcript";
import Welcome from "./welcome/Welcome";

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
  const [deleteTarget, setDeleteTarget] = useState<{
    sessionId: string;
    label: string;
  } | null>(null);
  // What the welcome screen of a new chat offers to look at.
  const { alerts } = useAlerts();

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
  // A side question takes the space beside the chat while it is open.
  const pane = sideShown ? null : chatPane(state);
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

  // New chat is the page's one action. The previous chats, the activity map
  // and a plugin's pane are tabs on the chat's edge (dock/AssistantDock), and the text size is in
  // the composer's menu.
  const actions = (
    <HeaderActions
      create={
        <AppActionIconButton
          ariaLabel="New chat"
          disabled={loadingOrRunning}
          icon="mdi:message-plus-outline"
          iconSize={20}
          label="New chat (Ctrl+Shift+O)"
          onClick={() => void assistantStore.newSession()}
        />
      }
    />
  );

  const textSize = (
    <div className="assistant__text-size">
      <AppTypography
        className="assistant__text-size-label"
        color="text.secondary"
        variant="body2"
      >
        Text size
      </AppTypography>
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
    </div>
  );

  // The plugin manager stays reachable when the composer is disabled: a
  // plugin can be what keeps the agent from starting.
  const managePlugins = agentEntry?.plugins ? (
    <AppButton onClick={() => setPluginsOpen(true)} variant="outlined">
      Manage plugins
    </AppButton>
  ) : null;

  return (
    <div
      className="assistant"
      style={{ "--app-font-size-body": `${fontSize}px` } as CSSProperties}
    >
      {/* Like the terminal: the chat's icons sit in the page above it, not
          in the shell header. */}
      <div className="assistant__actions">{actions}</div>
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
                <>
                  {managePlugins}
                  <AppButton onClick={() => void assistantStore.restart()}>
                    Restart
                  </AppButton>
                </>
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
                  <>
                    {managePlugins}
                    <AppButton onClick={() => void assistantStore.restart()}>
                      Restart
                    </AppButton>
                  </>
                ) : undefined
              }
            >
              {state.error}
            </AppAlert>
          ) : null}

          {/* A shell command in an answer can be handed to the terminal,
              which types it and leaves running it to the user. */}
          <OpenInTerminalContext
            value={(command) =>
              void navigate({
                to: "/terminal",
                state: { terminalCommand: command },
              })
            }
          >
            <Transcript
              blocks={state.blocks}
              pendingToolCallId={
                state.pending?.request.toolCall.toolCallId ?? null
              }
              sessionId={state.sessionId}
            >
              {!ready ? (
                <WidgetLoader label="Checking what is installed" />
              ) : null}
              {state.status === "connecting" ? (
                <WidgetLoader label={`Starting ${agentLabel ?? "the agent"}`} />
              ) : null}
              {state.status === "ready" ? (
                <Welcome
                  agentLabel={agentLabel ?? ""}
                  alerts={alerts}
                  onAsk={(prompt) =>
                    void assistantStore.send(buildPrompt(prompt, []), {
                      text: prompt,
                      attachments: [],
                    })
                  }
                />
              ) : null}
            </Transcript>
          </OpenInTerminalContext>

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
            menuExtra={textSize}
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
          <AssistantDock
            // A side question takes the space beside the chat while it is open.
            activity={
              sideShown
                ? null
                : {
                    blocks: state.blocks,
                    usage: state.usage,
                    agentLabel: agentLabel ?? "",
                    model: modelName(state.configOptions) ?? null,
                  }
            }
            busy={loadingOrRunning}
            canDelete={state.canDeleteSession}
            canList={state.canListSessions}
            canLoad={state.canLoadSession}
            currentId={state.sessionId}
            onDelete={setDeleteTarget}
            onSelect={(sessionId) =>
              void assistantStore.selectSession(sessionId)
            }
            pane={pane}
            running={status === "running"}
            sessions={state.sessions}
          />
        </section>
        {state.side?.open ? (
          <SidePanel side={state.side} />
        ) : state.sideOpening && !state.side ? (
          <SidePanelOpening />
        ) : null}
      </div>
    </div>
  );
}
