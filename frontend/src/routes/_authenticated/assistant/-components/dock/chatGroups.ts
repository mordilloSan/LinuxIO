import type * as acp from "@agentclientprotocol/sdk/unstable";

export interface ChatGroup {
  label: "Today" | "Yesterday" | "Earlier";
  sessions: acp.SessionInfo[];
}

/** What a chat is called: its title, or its session id while it has none. */
export const chatLabel = (session: acp.SessionInfo) =>
  session.title ?? session.sessionId;

/**
 * The chats whose label holds the search, under the day they were last used.
 * The list arrives newest first and keeps that order; a chat without a date
 * counts as earlier, and a day without chats is left out.
 */
export function groupChats(
  sessions: acp.SessionInfo[],
  search: string,
  now: number,
): ChatGroup[] {
  const today = new Date(now);
  today.setHours(0, 0, 0, 0);
  const yesterday = new Date(today);
  yesterday.setDate(today.getDate() - 1);
  const needle = search.trim().toLowerCase();

  const groups: ChatGroup[] = [
    { label: "Today", sessions: [] },
    { label: "Yesterday", sessions: [] },
    { label: "Earlier", sessions: [] },
  ];
  for (const session of sessions) {
    if (needle && !chatLabel(session).toLowerCase().includes(needle)) continue;
    const used = session.updatedAt ? Date.parse(session.updatedAt) : Number.NaN;
    const index =
      used >= today.getTime() ? 0 : used >= yesterday.getTime() ? 1 : 2;
    groups[index].sessions.push(session);
  }
  return groups.filter((group) => group.sessions.length > 0);
}
