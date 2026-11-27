import type * as acp from "@agentclientprotocol/sdk/unstable";
import { Icon } from "@iconify/react";
import { useRef, useState } from "react";

import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppButton from "@/components/ui/AppButton";
import AppSearchField from "@/components/ui/AppSearchField";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { formatRelativeAge } from "@/utils/formatters";

import { chatLabel, groupChats } from "./chatGroups";
import DockCard from "./DockCard";
import DockResize from "./DockResize";
import { useCardWidth } from "./dockWidth";

import "./dock.css";

/**
 * The previous chats on one of the dock's cards: searched by title, grouped
 * by the day they were last used, the open one marked, each renamed in
 * place where the agent can.
 */
export default function ChatsCard({
  open,
  onClose,
  sessions,
  currentId,
  now,
  canLoad,
  canDelete,
  canRename,
  busy,
  onSelect,
  onDelete,
  onRename,
  room,
  order,
}: {
  open: boolean;
  onClose: () => void;
  sessions: acp.SessionInfo[];
  currentId: string | null;
  /** The time the card was opened, which decides what counts as today. */
  now: number;
  canLoad: boolean;
  canDelete: boolean;
  canRename: boolean;
  /** A turn is running or the agent is starting: no other chat can open. */
  busy: boolean;
  onSelect: (sessionId: string) => void;
  onDelete: (chat: { sessionId: string; label: string }) => void;
  onRename: (sessionId: string, title: string) => void;
  /** The width of the chat area the card floats in; null until measured. */
  room: number | null;
  /** Where the card stands in the stack of open cards: the lower, the higher. */
  order?: number;
}) {
  const [search, setSearch] = useState("");
  // The chat whose title is being edited, and the one whose Rename button
  // takes the focus back when the edit ends from the keyboard.
  const [editing, setEditing] = useState<string | null>(null);
  const refocus = useRef<string | null>(null);
  const { width, maxWidth, setWidth } = useCardWidth(room);
  const groups = groupChats(sessions, search, now);

  return (
    <DockCard>
      <aside
        aria-hidden={!open}
        aria-label="Chats"
        className={`assistant-dock-card assistant-chats${open ? "" : " assistant-dock-card--closed"}`}
        inert={!open}
        style={{ width, order }}
      >
        <DockResize
          maxWidth={maxWidth}
          setWidth={setWidth}
          title="Chats"
          width={width}
        />
        <header className="assistant-dock-card__header">
          <AppTypography
            className="assistant-dock-card__title"
            fontWeight={600}
            noWrap
          >
            Chats
          </AppTypography>
          <AppActionIconButton
            ariaLabel="Close Chats"
            icon="mdi:close"
            onClick={onClose}
            size="small"
            tooltip={false}
          />
        </header>
        <div className="assistant-chats__search">
          <AppSearchField
            aria-label="Search chats"
            fullWidth
            onChange={(event) => setSearch(event.target.value)}
            placeholder="Search chats"
            startAdornment={<Icon height={18} icon="mdi:magnify" width={18} />}
            value={search}
          />
        </div>
        <div className="assistant-chats__list">
          {groups.map((group) => (
            <section className="assistant-chats__group" key={group.label}>
              <h2 className="assistant-chats__day">{group.label}</h2>
              {group.sessions.map((session) => {
                const label = chatLabel(session);
                const isCurrent = session.sessionId === currentId;
                return (
                  <div className="assistant-chats__row" key={session.sessionId}>
                    {editing === session.sessionId ? (
                      <ChatTitleField
                        onDone={(title, fromKeyboard) => {
                          if (title !== null)
                            onRename(session.sessionId, title);
                          if (fromKeyboard) refocus.current = session.sessionId;
                          setEditing(null);
                        }}
                        title={session.title ?? ""}
                      />
                    ) : (
                      <>
                        <AppButton
                          aria-current={isCurrent ? "true" : undefined}
                          className="assistant-chats__open"
                          color="inherit"
                          disabled={!canLoad || busy}
                          onClick={() => onSelect(session.sessionId)}
                        >
                          <span className="assistant-chats__title">
                            {label}
                          </span>
                          {session.updatedAt ? (
                            <span className="assistant-chats__age">
                              {formatRelativeAge(Date.parse(session.updatedAt))}
                            </span>
                          ) : null}
                        </AppButton>
                        {canRename ? (
                          <AppActionIconButton
                            ariaLabel={`Rename ${label}`}
                            className="assistant-chats__action"
                            icon="mdi:pencil-outline"
                            onClick={() => setEditing(session.sessionId)}
                            ref={(button) => {
                              if (
                                button &&
                                refocus.current === session.sessionId
                              ) {
                                refocus.current = null;
                                button.focus();
                              }
                            }}
                            size="small"
                            tooltip={false}
                          />
                        ) : null}
                        {canDelete ? (
                          <AppActionIconButton
                            ariaLabel={`Delete ${label}`}
                            className="assistant-chats__action"
                            disabled={isCurrent && busy}
                            icon="mdi:delete-outline"
                            onClick={() =>
                              onDelete({ sessionId: session.sessionId, label })
                            }
                            size="small"
                            tooltip={false}
                          />
                        ) : null}
                      </>
                    )}
                  </div>
                );
              })}
            </section>
          ))}
          {groups.length === 0 ? (
            <AppTypography
              className="assistant-chats__none"
              color="text.secondary"
            >
              {sessions.length === 0 ? "No previous chats" : "No chats match."}
            </AppTypography>
          ) : null}
        </div>
      </aside>
    </DockCard>
  );
}

/**
 * A chat's title, edited in place: Enter or leaving the field saves it,
 * Escape keeps the old one. An empty or unchanged title saves nothing.
 */
function ChatTitleField({
  title,
  onDone,
}: {
  title: string;
  /** The new title, or null when there is nothing to save. */
  onDone: (title: string | null, fromKeyboard: boolean) => void;
}) {
  const [draft, setDraft] = useState(title);
  // Enter and Escape end the edit before the field's blur would.
  const ended = useRef(false);
  const end = (save: boolean, fromKeyboard: boolean) => {
    if (ended.current) return;
    ended.current = true;
    const next = draft.trim();
    onDone(save && next !== "" && next !== title ? next : null, fromKeyboard);
  };

  return (
    <AppTextField
      aria-label="Chat title"
      autoFocus
      className="assistant-chats__rename"
      fullWidth
      onBlur={() => end(true, false)}
      onChange={(event) => setDraft(event.target.value)}
      onFocus={(event) => {
        if (event.target instanceof HTMLInputElement) event.target.select();
      }}
      onKeyDown={(event) => {
        if (event.key === "Enter") {
          event.preventDefault();
          end(true, true);
        } else if (event.key === "Escape") {
          // Escape here ends the edit, not the running turn.
          event.preventDefault();
          end(false, true);
        }
      }}
      size="small"
      value={draft}
    />
  );
}
