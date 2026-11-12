import type * as acp from "@agentclientprotocol/sdk/unstable";
import { Icon } from "@iconify/react";
import { useState } from "react";

import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppButton from "@/components/ui/AppButton";
import AppSearchField from "@/components/ui/AppSearchField";
import AppTypography from "@/components/ui/AppTypography";
import { formatRelativeAge } from "@/utils/formatters";

import { chatLabel, groupChats } from "./chatGroups";

import "./dock.css";

/**
 * The previous chats on one of the dock's cards: searched by title, grouped
 * by the day they were last used, the open one marked.
 */
export default function ChatsCard({
  open,
  onClose,
  sessions,
  currentId,
  now,
  canLoad,
  canDelete,
  busy,
  onSelect,
  onDelete,
  maxWidth,
}: {
  open: boolean;
  onClose: () => void;
  sessions: acp.SessionInfo[];
  currentId: string | null;
  /** The time the card was opened, which decides what counts as today. */
  now: number;
  canLoad: boolean;
  canDelete: boolean;
  /** A turn is running or the agent is starting: no other chat can open. */
  busy: boolean;
  onSelect: (sessionId: string) => void;
  onDelete: (chat: { sessionId: string; label: string }) => void;
  maxWidth?: number;
}) {
  const [search, setSearch] = useState("");
  const groups = groupChats(sessions, search, now);

  return (
    <aside
      aria-hidden={!open}
      aria-label="Chats"
      className={`assistant-dock-card assistant-chats${open ? "" : " assistant-dock-card--closed"}`}
      inert={!open}
      style={{ maxWidth }}
    >
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
                  <AppButton
                    aria-current={isCurrent ? "true" : undefined}
                    className="assistant-chats__open"
                    color="inherit"
                    disabled={!canLoad || busy}
                    onClick={() => onSelect(session.sessionId)}
                  >
                    <span className="assistant-chats__title">{label}</span>
                    {session.updatedAt ? (
                      <span className="assistant-chats__age">
                        {formatRelativeAge(Date.parse(session.updatedAt))}
                      </span>
                    ) : null}
                  </AppButton>
                  {canDelete ? (
                    <AppActionIconButton
                      ariaLabel={`Delete ${label}`}
                      className="assistant-chats__delete"
                      disabled={isCurrent && busy}
                      icon="mdi:delete-outline"
                      onClick={() =>
                        onDelete({ sessionId: session.sessionId, label })
                      }
                      size="small"
                      tooltip={false}
                    />
                  ) : null}
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
  );
}
