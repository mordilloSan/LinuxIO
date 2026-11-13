import type * as acp from "@agentclientprotocol/sdk/unstable";
import { useEffect, useRef, useState } from "react";

import type { PaneView } from "@/api/acp";
import {
  readPersistedState,
  writePersistedState,
} from "@/utils/persistedState";

import PanePanel from "../pane/PanePanel";
import type { TranscriptBlock } from "../transcript/transcript";
import AgentMap from "../workmap/AgentMap";
import { agentsOf, runningAgents } from "../workmap/agents";
import WorkMap from "../workmap/WorkMap";
import { summarize } from "../workmap/workMap";
import ChatsCard from "./ChatsCard";
import { DOCK_INSET, DOCK_TABS } from "./dockLayout";
import DockPanel from "./DockPanel";
import DockTab from "./DockTab";

import "./dock.css";

const ACTIVITY_KEY = "linuxio.assistant.activityOpen";
const VIEW_KEY = "linuxio.assistant.activityView";
const isFlag = (value: unknown): value is boolean => typeof value === "boolean";

/** The activity card draws the map, or the chat's agents. */
type ActivityView = "map" | "agents";
const isView = (value: unknown): value is ActivityView =>
  value === "map" || value === "agents";

/**
 * The tabs on the chat's left edge and the card each one opens: the previous
 * chats, the activity map, and a plugin's pane while there is one. One card
 * is open at a time, so they never run past the bottom of a short window.
 */
export default function AssistantDock({
  pane,
  activity,
  running,
  sessions,
  currentId,
  canList,
  canLoad,
  canDelete,
  busy,
  onSelect,
  onDelete,
}: {
  pane: PaneView | null;
  /** What the activity map draws; null leaves the map and its tab out. */
  activity: {
    blocks: readonly TranscriptBlock[];
    usage: { used: number; size: number } | null;
    agentLabel: string;
    /** The model the chat is set to, when the agent names one. */
    model: string | null;
  } | null;
  running: boolean;
  sessions: acp.SessionInfo[];
  currentId: string | null;
  canList: boolean;
  canLoad: boolean;
  canDelete: boolean;
  busy: boolean;
  onSelect: (sessionId: string) => void;
  onDelete: (chat: { sessionId: string; label: string }) => void;
}) {
  const dockRef = useRef<HTMLDivElement>(null);
  const chatsTabRef = useRef<HTMLButtonElement>(null);
  // The width of the chat area the cards float in; unknown until measured.
  const [room, setRoom] = useState<number | null>(null);
  // now is read when the card opens, and decides what counts as today.
  const [chats, setChats] = useState({ open: false, now: 0 });
  // Whether the user opened or closed a pane, which holds for that pane only.
  // Without a choice it follows the room: open where it fits beside the chat.
  const [paneChoice, setPaneChoice] = useState<{
    id: string;
    open: boolean;
  } | null>(null);
  const paneOpen = pane && paneChoice?.id === pane.id ? paneChoice.open : null;
  // The map opens on request. The browser remembers that it was open, and a
  // remembered map (null) comes back only where it fits beside the chat.
  const [mapOpen, setMapOpen] = useState<boolean | null>(() =>
    readPersistedState(ACTIVITY_KEY, isFlag) ? null : false,
  );
  const openMap = (open: boolean) => {
    setMapOpen(open);
    writePersistedState(ACTIVITY_KEY, open);
  };
  const [view, setView] = useState<ActivityView>(
    () => readPersistedState(VIEW_KEY, isView) ?? "map",
  );
  const showView = (next: ActivityView) => {
    setView(next);
    writePersistedState(VIEW_KEY, next);
  };
  // Opening the activity card closes whatever else is open.
  const openActivity = (next: ActivityView) => {
    showView(next);
    openMap(true);
    setChats({ ...chats, open: false });
    if (pane) setPaneChoice({ id: pane.id, open: false });
  };
  const summary = activity ? summarize(activity.blocks, running) : null;
  const agents = activity ? agentsOf(activity.blocks, running) : [];
  const busyAgents = runningAgents(agents);

  useEffect(() => {
    const area = dockRef.current?.parentElement;
    if (!area || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(([entry]) =>
      setRoom(entry.contentRect.width),
    );
    observer.observe(area);
    return () => observer.disconnect();
  }, []);

  return (
    <div className="assistant-dock" ref={dockRef}>
      <DockTab
        disabled={!canList}
        icon="mdi:history"
        label="Chats"
        onClick={() => {
          const open = !chats.open;
          setChats({ open, now: Date.now() });
          if (open && pane) setPaneChoice({ id: pane.id, open: false });
        }}
        open={chats.open}
        ref={chatsTabRef}
      />
      <ChatsCard
        busy={busy}
        canDelete={canDelete}
        canLoad={canLoad}
        currentId={currentId}
        maxWidth={
          room === null ? undefined : Math.max(0, room - DOCK_INSET - DOCK_TABS)
        }
        now={chats.now}
        onClose={() => {
          setChats({ ...chats, open: false });
          // The card goes inert; its tab is where it went.
          chatsTabRef.current?.focus();
        }}
        onDelete={onDelete}
        onSelect={onSelect}
        open={chats.open}
        sessions={sessions}
      />
      {activity && summary ? (
        <DockPanel
          also={{
            label: `${busyAgents} ${busyAgents === 1 ? "agent" : "agents"}`,
            current: view === "agents",
            running: busyAgents > 0,
            quiet: busyAgents === 0,
            onOpen: () => openActivity("agents"),
          }}
          // The map repeats the transcript, so it is only drawn while open.
          drawClosed={false}
          drawing={summary.total}
          heading={view === "agents" ? "Agents" : "Activity"}
          onOpenChange={(open) => {
            if (open) openActivity("map");
            else openMap(false);
          }}
          open={chats.open ? false : mapOpen}
          room={room}
          running={running}
          title="Activity"
        >
          {view === "agents" ? (
            <AgentMap
              agentLabel={activity.agentLabel}
              agents={agents}
              model={activity.model}
              onMain={() => showView("map")}
              running={running}
              usage={activity.usage}
            />
          ) : (
            <WorkMap
              agentLabel={activity.agentLabel}
              model={activity.model}
              onAgents={() => showView("agents")}
              running={running}
              summary={summary}
              usage={activity.usage}
            />
          )}
        </DockPanel>
      ) : null}
      {pane ? (
        <PanePanel
          onOpenChange={(open) => {
            setPaneChoice({ id: pane.id, open });
            if (!open) return;
            setChats({ ...chats, open: false });
            if (activity) openMap(false);
          }}
          // A pane left to follow the room stays closed under an open map.
          open={
            chats.open || (activity && mapOpen !== false) ? false : paneOpen
          }
          pane={pane}
          room={room}
          running={running}
        />
      ) : null}
    </div>
  );
}
