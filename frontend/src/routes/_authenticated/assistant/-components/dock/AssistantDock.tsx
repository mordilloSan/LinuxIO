import type * as acp from "@agentclientprotocol/sdk/unstable";
import {
  type Ref,
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
} from "react";

import type { PaneView } from "@/api/acp";
import {
  readPersistedState,
  writePersistedState,
} from "@/utils/persistedState";

import PanePanel from "../pane/PanePanel";
import type { Subagent } from "../subagents";
import type { TranscriptBlock } from "../transcript/transcript";
import AgentMap from "../workmap/AgentMap";
import { agentsOf, runningAgents } from "../workmap/agents";
import WorkMap from "../workmap/WorkMap";
import { summarize } from "../workmap/workMap";
import ChatsCard from "./ChatsCard";
import { DockCardsHost } from "./DockCard";
import DockPanel from "./DockPanel";
import DockTab from "./DockTab";
import { DockWidthProvider } from "./dockWidth";

import "./dock.css";

const ACTIVITY_KEY = "linuxio.assistant.activityOpen";
const AGENTS_KEY = "linuxio.assistant.agentsOpen";
const isFlag = (value: unknown): value is boolean => typeof value === "boolean";

// The cards stand in this order from the top, whatever order they were opened
// in, so a card is always where the user last saw it.
const ORDER = { chats: 0, agents: 1, activity: 2, pane: 3 } as const;

/** What the page can ask of the dock. */
export interface AssistantDockHandle {
  /** Opens the Agents card on one subagent, by its session id. */
  showAgent(id: string): void;
}

/**
 * The tabs on the chat's right edge and the card each one opens: the previous
 * chats, the activity map, the chat's agents, and a plugin's pane while
 * there is one. Opening a
 * card leaves the others as they are: the open cards stack beside the tabs in
 * a fixed order (chats, agents, activity, then a pane), and closing one closes
 * the gap. They share one width, which the user can change, and on a short
 * window they shrink and scroll inside.
 */
export default function AssistantDock({
  ref,
  pane,
  activity,
  running,
  sessions,
  currentId,
  canList,
  canLoad,
  canDelete,
  canRename,
  busy,
  onSelect,
  onDelete,
  onRename,
  onStopAgent,
}: {
  ref?: Ref<AssistantDockHandle>;
  pane: PaneView | null;
  /** What the activity map draws; null leaves the map and its tab out. */
  activity: {
    sessionId: string | null;
    blocks: readonly TranscriptBlock[];
    subagents: readonly Subagent[];
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
  canRename: boolean;
  busy: boolean;
  onSelect: (sessionId: string) => void;
  onDelete: (chat: { sessionId: string; label: string }) => void;
  onRename: (sessionId: string, title: string) => void;
  /** Stops one subagent, by its session id. */
  onStopAgent: (id: string) => void;
}) {
  const dockRef = useRef<HTMLDivElement>(null);
  const chatsTabRef = useRef<HTMLButtonElement>(null);
  // Where the cards stack; the cards place themselves in it once it exists.
  const [cardsHost, setCardsHost] = useState<HTMLDivElement | null>(null);
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
  // The map and the agents open on request. The browser remembers that each
  // was open, and a remembered card (null) comes back only where it fits
  // beside the chat.
  const [mapOpen, setMapOpen] = useState<boolean | null>(() =>
    readPersistedState(ACTIVITY_KEY, isFlag) ? null : false,
  );
  const openMap = (open: boolean) => {
    setMapOpen(open);
    writePersistedState(ACTIVITY_KEY, open);
  };
  const [agentsOpen, setAgentsOpen] = useState<boolean | null>(() =>
    readPersistedState(AGENTS_KEY, isFlag) ? null : false,
  );
  const openAgents = (open: boolean) => {
    setAgentsOpen(open);
    writePersistedState(AGENTS_KEY, open);
  };
  // The subagent the agents card is open on; null shows them all.
  const [agentId, setAgentId] = useState<string | null>(null);
  useImperativeHandle(ref, () => ({
    showAgent(id) {
      setAgentId(id);
      openAgents(true);
    },
  }));
  const summary = activity
    ? summarize(activity.blocks, running, activity.subagents)
    : null;
  const agents = activity
    ? agentsOf(activity, activity.subagents, running)
    : [];
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
      <div className="assistant-dock-cards" ref={setCardsHost} />
      <DockWidthProvider>
        <DockCardsHost value={cardsHost}>
          <DockTab
            disabled={!canList}
            icon="mdi:history"
            label="Chats"
            onClick={() => {
              const open = !chats.open;
              setChats({ open, now: Date.now() });
            }}
            open={chats.open}
            ref={chatsTabRef}
          />
          <ChatsCard
            busy={busy}
            canDelete={canDelete}
            canLoad={canLoad}
            canRename={canRename}
            currentId={currentId}
            now={chats.now}
            order={ORDER.chats}
            onClose={() => {
              setChats({ ...chats, open: false });
              // The card goes inert; its tab is where it went.
              chatsTabRef.current?.focus();
            }}
            onDelete={onDelete}
            onRename={onRename}
            onSelect={onSelect}
            open={chats.open}
            room={room}
            sessions={sessions}
          />
          {activity && summary ? (
            <>
              <DockPanel
                drawClosed={false}
                drawing={activity.subagents}
                heading="Agents"
                onOpenChange={openAgents}
                open={agentsOpen}
                order={ORDER.agents}
                quiet={busyAgents === 0}
                room={room}
                running={busyAgents > 0}
                title={`${busyAgents} ${busyAgents === 1 ? "agent" : "agents"}`}
              >
                <AgentMap
                  agentLabel={activity.agentLabel}
                  agents={agents}
                  chosenId={agentId}
                  model={activity.model}
                  onChoose={setAgentId}
                  onMain={() => openMap(true)}
                  onStop={onStopAgent}
                  running={running}
                  usage={activity.usage}
                />
              </DockPanel>
              <DockPanel
                // The map repeats the transcript, so it is only drawn while
                // open.
                drawClosed={false}
                drawing={summary.total}
                heading="Activity"
                onOpenChange={openMap}
                open={mapOpen}
                order={ORDER.activity}
                room={room}
                running={running}
                title="Activity"
              >
                <WorkMap
                  agentLabel={activity.agentLabel}
                  model={activity.model}
                  onAgents={() => openAgents(true)}
                  running={running}
                  summary={summary}
                  usage={activity.usage}
                />
              </DockPanel>
            </>
          ) : null}
          {pane ? (
            <PanePanel
              onOpenChange={(open) => {
                setPaneChoice({ id: pane.id, open });
              }}
              open={paneOpen}
              order={ORDER.pane}
              pane={pane}
              room={room}
              running={running}
            />
          ) : null}
        </DockCardsHost>
      </DockWidthProvider>
    </div>
  );
}
