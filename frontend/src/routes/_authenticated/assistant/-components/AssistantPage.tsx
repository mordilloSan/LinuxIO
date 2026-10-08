import { useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { createPortal } from "react-dom";

import { useStreamMux } from "@/api";
import { ASSISTANT_AGENTS, type AssistantAgentId } from "@/api/acp/agents";
import ConfirmDialog from "@/components/filebrowser/ConfirmDialog";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import { OVERLAY_ROOT_SELECTOR } from "@/components/ui/AppDialog";
import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";
import AppTypography from "@/components/ui/AppTypography";
import HeaderActions from "@/components/ui/HeaderActions";
import { useHeaderActionSlot } from "@/contexts/HeaderActionSlotContext";

import "./assistant.css";

import { assistantStore, useAssistantState } from "./assistant-store";
import Composer from "./Composer";
import PermissionCard from "./PermissionCard";
import SidePanel, { SidePanelOpening } from "./SidePanel";
import StatusLine from "./StatusLine";
import Transcript from "./Transcript";

// Escape inside an overlay belongs to that overlay, not to the running turn.
const OVERLAY_SELECTOR = `${OVERLAY_ROOT_SELECTOR}, .app-popover-root, [role="menu"], [role="listbox"]`;

export default function AssistantPage({
  agent,
}: {
  agent: AssistantAgentId | null;
}) {
  const { isOpen } = useStreamMux();
  const state = useAssistantState();
  const navigate = useNavigate();
  const slot = useHeaderActionSlot();
  const [historyAnchor, setHistoryAnchor] = useState<HTMLElement | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<{
    sessionId: string;
    label: string;
  } | null>(null);

  useEffect(() => {
    if (agent && isOpen) void assistantStore.connect(agent);
  }, [agent, isOpen]);

  const { status } = state;
  const sideRunning = state.side?.status === "running";
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
      <AppAlert severity="info">
        Choose an agent in Settings → Assistant to start a conversation.
      </AppAlert>
    );
  }

  const actions = (
    <HeaderActions
      options={
        <>
          <AppActionIconButton
            ariaLabel="Previous chats"
            disabled={!state.canListSessions}
            icon="mdi:history"
            iconSize={20}
            label="Previous chats"
            onClick={(event) => setHistoryAnchor(event.currentTarget)}
          />
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
    <div className="assistant">
      {slot?.host ? (
        createPortal(actions, slot.host)
      ) : (
        <div className="assistant__actions">{actions}</div>
      )}
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
        confirmText="Delete"
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

      <div
        className={`assistant__body${state.side?.open || (state.sideOpening && !state.side) ? " assistant__body--split" : ""}`}
      >
        <section className="assistant__main">
          {state.status === "connecting" ? (
            <AppTypography color="text.secondary">
              Starting{" "}
              {ASSISTANT_AGENTS.find((entry) => entry.id === agent)?.label ??
                agent}
              …
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
                  I have logged in
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

          <Transcript blocks={state.blocks} sessionId={state.sessionId} />

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
        ) : null}
      </div>
    </div>
  );
}
