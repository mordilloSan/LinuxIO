/**
 * Where a chat's work goes, read from the transcript the page already holds:
 * each block is sorted to one station of the activity map. Nothing here asks
 * the agent for anything, so it works for every agent that reports tool calls.
 *
 * The stations, their icons, the wording of the rows and the picture drawn
 * from this (WorkMap.tsx, work-map.css) follow lot-map by Aleksei Volodin
 * (https://github.com/alexeyw/lot-map), used under the MIT licence recorded
 * in docs/THIRD_PARTY_NOTICES.md.
 */
import { isActive, sentIn, type Subagent, workFor } from "../subagents";
import {
  stringField,
  type ToolBlock,
  toolEnded,
  type TranscriptBlock,
} from "../transcript/transcript";
import { commandName, shellStation } from "./shell";

const ring = (x: number, y: number, r: number) =>
  `M${x - r} ${y}a${r} ${r} 0 1 0 ${2 * r} 0a${r} ${r} 0 1 0 ${-2 * r} 0`;

// In the order they stand around the agent, clockwise from the top. Each icon
// is a path around the station's centre.
export const STATIONS = [
  {
    id: "you",
    label: "You",
    icon: `${ring(0, -3.6, 3.4)}M-6.5 8c0-4 3-6.2 6.5-6.2s6.5 2.2 6.5 6.2`,
  },
  {
    id: "model",
    label: "Model",
    icon: "M-5 -5h10v10h-10zM-2 -8.5v3.5M2 -8.5v3.5M-2 5v3.5M2 5v3.5M-8.5 -2h3.5M-8.5 2h3.5M5 -2h3.5M5 2h3.5",
  },
  {
    id: "memory",
    label: "Memory",
    icon: `M-5 4.5L0 -5L6 3.5M-5 4.5L6 3.5${ring(-5, 4.5, 2.1)}${ring(0, -5, 2.1)}${ring(6, 3.5, 2.1)}`,
  },
  {
    id: "files",
    label: "Files",
    icon: "M-5.5 -8h7.5l3.5 3.5v12.5h-11zM2 -8v3.5h3.5",
  },
  { id: "shell", label: "Shell", icon: "M-7.5 -5l5 5l-5 5M0 6h7.5" },
  {
    id: "network",
    label: "Network",
    icon: `${ring(0, 0, 7.8)}M-7.8 0h15.6M0 -7.8c-4.2 4.2 -4.2 11.4 0 15.6M0 -7.8c4.2 4.2 4.2 11.4 0 15.6`,
  },
  {
    id: "browser",
    label: "Browser",
    icon: "M-8.5 -6.5h17v13h-17zM-8.5 -2.8h17M-6.3 -4.7h.1M-3.8 -4.7h.1",
  },
  {
    id: "agents",
    label: "Agents",
    icon: `M0 -4v3.5M0 -.5L-6 4M0 -.5L6 4${ring(0, -6, 2.1)}${ring(-6.5, 5.5, 2.1)}${ring(6.5, 5.5, 2.1)}`,
  },
  {
    id: "skills",
    label: "Skills",
    icon: "M1.5 -8.5L-5.5 1h4.5l-1.5 7.5l7.5 -9.8h-4.5z",
  },
  // Settings hooks never reach the browser (the adapter drops them), so this
  // station stays quiet until the relay passes them on.
  {
    id: "hooks",
    label: "Hooks",
    icon: `M2.5 -5.5v8a4.2 4.2 0 1 1 -8.4 0v-1.5${ring(2.5, -7.3, 1.8)}`,
  },
] as const;

export type StationId = (typeof STATIONS)[number]["id"];

export interface StationTally {
  /** Everything that happened at the station. */
  count: number;
  /** Calls of the running turn that have not ended. */
  open: number;
  /** Calls that failed since the last prompt. */
  faults: number;
  /**
   * What LinuxIO saw happen (it carries a time), which is what the map
   * animates: a chat that was loaded has none of it. At You, `prompts` of
   * what came `back` and `answers` of what went `out` are the user's prompts
   * and the agent's answers, which play apart from the station's calls.
   */
  seen: {
    out: number;
    back: number;
    failed: number;
    prompts: number;
    answers: number;
  };
}

export interface WorkRow {
  id: string;
  station: StationId;
  label: string;
  status: "run" | "ok" | "err" | "switch";
  /**
   * How many calls the row stands for: finished calls in a row at one
   * station, or, on the last of the calls still running past the list, those
   * and the ones that still run beyond it.
   */
  times: number;
  /**
   * How long it took, where that was seen happening; 0 for a moment, such as
   * a message arriving, which takes no time.
   */
  tookMs: number | null;
}

export interface WorkSummary {
  tally: Record<StationId, StationTally>;
  /** Prompts, steps of the model and tool calls, newest first. */
  rows: WorkRow[];
  /** What is running now, e.g. `Shell: List disks and 1 more`; null when nothing is. */
  now: string | null;
  /**
   * The stations the work went through last, oldest first, a station visited
   * twice in a row counted once. Only what was seen happening.
   */
  route: { station: StationId; at: number }[];
  /** Every event counted, so a caller can tell that something happened. */
  total: number;
}

const ROWS = 16;
// Calls still running past the last row take this many rows at most.
const HELD = ROWS / 2;
const ROUTE_STOPS = 7;

// Claude Code's tool names, which the adapter passes beside the ACP kind.
const NAMED: Record<string, StationId> = {
  Agent: "agents",
  Artifact: "network",
  // Work kept on claude.ai: an artifact's checks, comments and data, a
  // design, a shared guide, a published plugin.
  ArtifactCheck: "network",
  ArtifactComments: "network",
  ArtifactData: "network",
  AskUserQuestion: "you",
  // Work that runs later, apart from the turn: schedules and routines.
  CronCreate: "agents",
  CronDelete: "agents",
  CronList: "agents",
  DesignSync: "network",
  // What is shown to or asked of the user: the plan, the task list, a
  // notification, review findings.
  EnterPlanMode: "you",
  ExitPlanMode: "you",
  Glob: "files",
  Grep: "files",
  ListAgents: "agents",
  // Code intelligence from a language server.
  LSP: "files",
  PublishPlugin: "network",
  PushNotification: "you",
  RemoteTrigger: "agents",
  ReportFindings: "you",
  ScheduleWakeup: "agents",
  SendMessage: "agents",
  SendUserFile: "you",
  SendUserMessage: "you",
  ShareOnboardingGuide: "network",
  Skill: "skills",
  Task: "agents",
  TaskCreate: "you",
  TaskGet: "you",
  TaskList: "you",
  TaskStop: "agents",
  TaskUpdate: "you",
  TodoWrite: "you",
  ToolSearch: "skills",
  WebFetch: "network",
  WebSearch: "network",
  Workflow: "agents",
  memory_recall: "memory",
};
const FILE_TOOLS = new Set(["Read", "Write", "Edit", "NotebookEdit"]);
const SHELL_TOOLS = new Set(["Bash", "PowerShell", "Monitor"]);
// An MCP server's resources, which go where that server's tools go.
const MCP_RESOURCE_TOOLS = new Set([
  "ListMcpResourcesTool",
  "ReadMcpResourceTool",
]);

const FILE_KINDS = new Set(["read", "edit", "delete", "move", "search"]);

// The agent's own notes: a file in the folder, or the folder named in a
// command line. A chat keeps them in its LinuxIO profile, which a command
// line may name by its variable; `~/.claude` holds the notes of the user's
// Claude Code outside LinuxIO.
const MEMORY_PATH =
  /\/(?:linuxio\/profiles\/claude|\.claude)\/projects\/[^/]+\/memory\//;
const MEMORY_FOLDER =
  /(?:\/linuxio\/profiles\/claude|\/\.claude|\$\{?CLAUDE_CONFIG_DIR\}?)\/projects\/[^/\s]+\/memory(?![\w.-])/;
const BROWSER_SERVERS = new Set(["Claude_Browser", "claude-in-chrome"]);

/** A command line that only handles files works on notes when it names them. */
function commandStation(command: string | null): StationId {
  const station = shellStation(command);
  return station === "files" && command !== null && MEMORY_FOLDER.test(command)
    ? "memory"
    : station;
}

export const pathOf = (input: unknown) =>
  stringField(input, "file_path") ??
  stringField(input, "notebook_path") ??
  stringField(input, "path");

const fileStation = (input: unknown): StationId => {
  const path = pathOf(input);
  return path !== null && MEMORY_PATH.test(path) ? "memory" : "files";
};

export const mcpOf = (name: string) => {
  const match = /^mcp__([^_].*?)__(.+)$/.exec(name);
  return match ? { server: match[1], tool: match[2] } : null;
};

// A cloud session linked to the user's computer reaches the same browser
// under `remote-devices`.
const REMOTE_BROWSER = /^Claude_Browser__(.+)$/;
const mcpCall = (name: string) => {
  const mcp = mcpOf(name);
  const remote =
    mcp?.server === "remote-devices" ? REMOTE_BROWSER.exec(mcp.tool) : null;
  return remote ? { server: "Claude_Browser", tool: remote[1] } : mcp;
};

function mcpStation(server: string): StationId {
  if (/memory/i.test(server)) return "memory";
  if (BROWSER_SERVERS.has(server)) return "browser";
  if (server === "terminal") return "shell";
  if (server === "visualize" || server.startsWith("ccd_")) return "you";
  if (server === "plugin-authoring") return "skills";
  return "network";
}

export function stationOf(block: ToolBlock): StationId {
  const name = block.toolName;
  if (name) {
    if (FILE_TOOLS.has(name)) return fileStation(block.rawInput);
    if (SHELL_TOOLS.has(name)) {
      return name === "Monitor"
        ? "shell"
        : commandStation(stringField(block.rawInput, "command"));
    }
    if (MCP_RESOURCE_TOOLS.has(name)) {
      return mcpStation(stringField(block.rawInput, "server") ?? "");
    }
    if (name in NAMED) return NAMED[name];
    const mcp = mcpCall(name);
    return mcp ? mcpStation(mcp.server) : "shell";
  }
  // An agent that sends no tool name is sorted by the ACP kind alone.
  if (FILE_KINDS.has(block.toolKind)) return fileStation(block.rawInput);
  switch (block.toolKind) {
    case "execute":
      return commandStation(stringField(block.rawInput, "command"));
    case "fetch":
      return "network";
    case "think":
      return "model";
    default:
      return "shell";
  }
}

const lastTwo = (path: string) =>
  path.replaceAll("\\", "/").split("/").filter(Boolean).slice(-2).join("/");

// The host of an address, also of one written without its scheme, and
// nothing when it has none: the rest of it is never a label.
const hostOf = (url: string) => {
  for (const address of url.includes("://") ? [url] : [url, `https://${url}`]) {
    try {
      const { host } = new URL(address);
      if (host) return host;
    } catch {
      // Not an address as written; the next form may be.
    }
  }
  return "";
};

export const spaced = (name: string) => name.replace(/_+/g, " ");

/** What a call is, in a few words: a name, never the whole input. */
export function labelOf(block: ToolBlock): string {
  const input = block.rawInput;
  const name = block.toolName;
  const described = stringField(input, "description");
  if (!name) return described ?? block.title;
  if (FILE_TOOLS.has(name)) return lastTwo(pathOf(input) ?? "") || name;
  if (SHELL_TOOLS.has(name)) {
    return (
      described ?? commandName(stringField(input, "command") ?? "") ?? name
    );
  }
  switch (name) {
    case "WebFetch":
      return hostOf(stringField(input, "url") ?? "") || name;
    case "WebSearch":
      return "web search";
    case "Artifact":
      return "artifact";
    case "Agent":
    case "Task":
      return described ?? "subagent";
    case "Workflow":
      return "workflow";
    case "Skill":
      return stringField(input, "skill") ?? "skill";
    case "ToolSearch":
      return "loading tools";
    case "AskUserQuestion":
      return "question for you";
  }
  const mcp = mcpCall(name);
  if (!mcp) return name;
  return mcpStation(mcp.server) === "network"
    ? `${mcp.server}: ${spaced(mcp.tool)}`
    : spaced(mcp.tool);
}

const LABEL = Object.fromEntries(
  STATIONS.map((station) => [station.id, station.label]),
) as Record<StationId, string>;

export const stationLabel = (id: StationId) => LABEL[id];

/**
 * 113 ms, 13.4 s, 2 min 5 s, 1 h 5 min. The time is rounded before its unit
 * is chosen, so no part reaches the size of the next unit: never `60.0 s` or
 * `1 min 60 s`.
 */
export function formatMs(ms: number): string {
  const tenths = Math.round(ms / 100);
  const seconds = Math.round(ms / 1000);
  if (Math.round(ms) < 1000) return `${Math.max(1, Math.round(ms))} ms`;
  if (tenths < 600) return `${(tenths / 10).toFixed(1)} s`;
  if (seconds < 3600) {
    return `${Math.floor(seconds / 60)} min ${seconds % 60} s`;
  }
  const minutes = Math.round(ms / 60_000);
  return `${Math.floor(minutes / 60)} h ${minutes % 60} min`;
}

const MARK = { switch: "◆", run: "●", ok: "·", err: "✕" } as const;
/** The pane text tone of a row, by its status. */
export const ROW_TONE = {
  switch: "accent",
  run: "accent",
  ok: "dim",
  err: "error",
} as const;
/** The classes of a row of the feed. */
export const ROW_TEXT =
  "assistant-pane-text assistant-pane-text--mono assistant-pane-text--small";
// The feed's second column fits the longest station name.
const COLUMN = Math.max(8, ...STATIONS.map((station) => station.label.length));
/** A label longer than this may not fit the card, so its row can be opened. */
export const LONG_LABEL = 28;

/** A row of the feed without its time: `· Files    disks/lsblk.go  +2`. */
export function rowLine(row: WorkRow): string {
  return `${MARK[row.status]} ${stationLabel(row.station).padEnd(COLUMN)} ${row.label}${row.times > 1 ? `  +${row.times - 1}` : ""}`;
}

/**
 * The time column of a row: `running`, how long it took, `—` for a moment,
 * and nothing where its time was not seen.
 */
export function rowTime(row: WorkRow): string {
  if (row.status === "run") return "running";
  if (row.tookMs === null) return "";
  return row.tookMs > 0 ? formatMs(row.tookMs) : "—";
}

/** How long from one time to another, when both were seen. */
export const span = (from: number | undefined, to: number | undefined) =>
  from === undefined || to === undefined ? null : Math.max(0, to - from);

type Entry = WorkRow & {
  /** A finished call or step, which folds into the one before it. */
  plain: boolean;
  /** When it was seen happening, for the route. */
  at: number | undefined;
  /** A subagent, or a call one made: not one of the chat's own blocks. */
  sub?: boolean;
  /** The session of the subagent whose doing it is; none for the chat's. */
  by?: string;
};

/**
 * Finished calls in a row at one station, by one session, read as one row,
 * the newest: the calls of two subagents never share a row. A call still
 * running is never pushed out by what came after it: past the last row it
 * stays, in place of the oldest rows that no longer run, for half the list at
 * most, the last of them standing for any more (`+N`).
 */
function fold(entries: readonly Entry[]): WorkRow[] {
  const rows: Entry[] = [];
  const held: Entry[] = [];
  // Once a row was left out, what comes after it is no neighbour of the last.
  let cut = false;
  for (let i = entries.length - 1; i >= 0; i--) {
    const entry = entries[i];
    const last = rows[rows.length - 1];
    if (
      !cut &&
      last?.plain &&
      entry.plain &&
      last.station === entry.station &&
      last.by === entry.by
    ) {
      last.times++;
      if (entry.tookMs !== null) {
        last.tookMs = (last.tookMs ?? 0) + entry.tookMs;
      }
      continue;
    }
    if (rows.length === ROWS) {
      cut = true;
      if (entry.status !== "run") continue;
      if (held.length < HELD) held.push({ ...entry });
      else held[held.length - 1].times++;
      continue;
    }
    rows.push({ ...entry });
  }
  // The held calls take the place of the oldest rows that no longer run;
  // those that still find no room count on the last held row that does.
  for (let i = rows.length - 1; i >= 0; i--) {
    if (rows.length + held.length <= ROWS) break;
    if (rows[i].status !== "run") rows.splice(i, 1);
  }
  const room = ROWS - rows.length;
  for (const extra of held.splice(room)) {
    if (room > 0) held[room - 1].times += extra.times;
  }
  return [...rows, ...held].map(
    ({ plain: _plain, at: _at, sub: _sub, by: _by, ...row }) => row,
  );
}

/**
 * The entries in the order they were seen: what the subagents did goes
 * among the chat's own entries by its time. The chat's own keep the order of
 * its blocks, and an entry that carries no time (a chat that was loaded)
 * stays where it stands, after the one before it.
 */
function inOrderSeen(entries: readonly Entry[]): Entry[] {
  let before = 0;
  let own = 0;
  return entries
    .map((entry) => {
      before = entry.at ?? before;
      if (!entry.sub) before = own = Math.max(own, before);
      return { entry, at: before };
    })
    .sort((a, b) => a.at - b.at)
    .map(({ entry }) => entry);
}

function routeOf(entries: readonly Entry[]): WorkSummary["route"] {
  const stops: WorkSummary["route"] = [];
  for (const entry of entries) {
    if (entry.station === "model" || entry.at === undefined) continue;
    const last = stops[stops.length - 1];
    if (last?.station === entry.station) last.at = entry.at;
    else stops.push({ station: entry.station, at: entry.at });
  }
  return stops.slice(-ROUTE_STOPS);
}

/**
 * Where the running turn starts: blocks after the last finished turn belong to
 * it. Without a running turn there is none, and no call is still running.
 */
export const liveFrom = (
  blocks: readonly TranscriptBlock[],
  running: boolean,
) =>
  running
    ? blocks.findLastIndex((block) => block.kind === "turn") + 1
    : blocks.length;

/**
 * `running` says a turn is live: only then is a call that has not ended
 * still running, and is the model at work whenever none of its calls is.
 * A subagent's calls count at their stations like the chat's own, and stand
 * after the message that set them off; the steps are the chat's model's only.
 */
export function summarize(
  blocks: readonly TranscriptBlock[],
  running: boolean,
  subagents: readonly Subagent[] = [],
): WorkSummary {
  const tally = Object.fromEntries(
    STATIONS.map((station) => [
      station.id,
      {
        count: 0,
        open: 0,
        faults: 0,
        seen: { out: 0, back: 0, failed: 0, prompts: 0, answers: 0 },
      },
    ]),
  ) as Record<StationId, StationTally>;
  const entries: Entry[] = [];
  const live = liveFrom(blocks, running);
  // A step of the model runs from when it was last handed something (a
  // prompt, or the results of its calls) to its first call or the turn's end.
  let step = 0;
  let model: string | undefined;
  let handedAt: number | undefined;
  let open: Entry | null = null;
  let openFrom: number | undefined;
  const openStep = (id: string) => {
    step++;
    open = {
      id,
      station: "model",
      label: model ? `${model} · step ${step}` : `step ${step}`,
      status: "ok",
      times: 1,
      tookMs: null,
      plain: true,
      at: handedAt,
    };
    openFrom = handedAt;
    entries.push(open);
    tally.model.count++;
    if (handedAt !== undefined) tally.model.seen.out++;
  };
  const closeStep = (at: number | undefined) => {
    if (!open) return;
    open.tookMs = span(openFrom, at);
    open = null;
    if (at !== undefined) tally.model.seen.back++;
  };

  // A call, the chat's or a subagent's (`by`); `runs` says it still runs.
  const call = (block: ToolBlock, runs: boolean, by?: string) => {
    const id = stationOf(block);
    const station = tally[id];
    const failed = block.status === "failed";
    station.count++;
    if (runs) station.open++;
    if (failed) station.faults++;
    if (block.startedAt !== undefined) station.seen.out++;
    if (block.endedAt !== undefined) {
      station.seen.back++;
      if (failed) station.seen.failed++;
    }
    entries.push({
      id: block.id,
      station: id,
      label: labelOf(block),
      status: runs ? "run" : failed ? "err" : "ok",
      times: 1,
      tookMs: span(block.startedAt, block.endedAt),
      plain: !runs && !failed,
      at: block.startedAt ?? block.endedAt,
      sub: by !== undefined,
      by,
    });
  };

  const drawn = new Set<Subagent>();
  // A subagent, started by the chat or by the subagent `by`.
  const subagent = (child: Subagent, at: number | undefined, by?: string) => {
    drawn.add(child);
    const station = tally.agents;
    const active = isActive(child.state);
    const failed = child.state === "failed";
    const closed = child.runs.filter((run) => run.endedAt !== null);
    station.count++;
    if (active) station.open++;
    if (failed) station.faults++;
    station.seen.out += child.runs.length;
    station.seen.back += closed.length;
    if (failed && closed.length > 0) station.seen.failed++;
    entries.push({
      id: child.sessionId,
      station: "agents",
      label: child.title ?? child.description ?? "subagent",
      status: active ? "run" : failed ? "err" : "ok",
      times: 1,
      tookMs:
        closed.length === 0
          ? null
          : closed.reduce(
              (sum, run) =>
                sum + (span(run.startedAt, run.endedAt ?? undefined) ?? 0),
              0,
            ),
      plain: !active && !failed,
      at: child.runs[0]?.startedAt ?? at,
      sub: true,
      by,
    });
  };
  // What a subagent did: its calls, and the subagents it started in turn.
  const work = (child: Subagent, list: readonly TranscriptBlock[]) => {
    const sent = sentIn(child.blocks);
    const active = isActive(child.state);
    for (const block of list) {
      if (block.kind === "tool") {
        // A subagent call with the id of a message was shown by a permission
        // request: the subagent it started stands for it.
        if (!sent.has(block.toolCallId)) {
          call(block, !toolEnded(block.status) && active, child.sessionId);
        }
      } else if (block.kind === "message" && block.outgoing) {
        started(
          block.recipientSessionId,
          block.messageId,
          block.at,
          sent,
          child.sessionId,
        );
      }
    }
  };
  // A message a session sent (the chat, or the subagent `by`): to a subagent,
  // it sets that subagent's work off.
  const started = (
    recipient: string | null,
    messageId: string,
    at: number | undefined,
    sent: ReadonlySet<string>,
    by?: string,
  ) => {
    const child = subagents.find((entry) => entry.sessionId === recipient);
    if (!child) return;
    if (!drawn.has(child)) subagent(child, at, by);
    work(child, workFor(child, messageId, sent));
  };
  const sent = sentIn(blocks);

  blocks.forEach((block, index) => {
    switch (block.kind) {
      case "user": {
        closeStep(undefined);
        step = 0;
        handedAt = block.at;
        for (const station of STATIONS) tally[station.id].faults = 0;
        tally.you.count++;
        if (block.at !== undefined) {
          tally.you.seen.back++;
          tally.you.seen.prompts++;
        }
        entries.push({
          id: block.id,
          station: "you",
          label: "new message",
          status: "ok",
          times: 1,
          tookMs: 0,
          // A prompt is a row of its own, never folded with a call at You.
          plain: false,
          at: block.at,
        });
        const next = block.model?.toLowerCase();
        if (next && model && next !== model) {
          entries.push({
            id: `${block.id}:switch`,
            station: "model",
            label: `${model} → ${next}`,
            status: "switch",
            times: 1,
            tookMs: 0,
            plain: false,
            at: block.at,
          });
        }
        model = next ?? model;
        break;
      }
      case "agent":
      case "thought": {
        const before = blocks[index - 1]?.kind;
        if (!open && before !== "agent" && before !== "thought") {
          openStep(`${block.id}:step`);
        }
        break;
      }
      case "tool": {
        if (sent.has(block.toolCallId)) break;
        if (block.opensStep && !open) openStep(`${block.id}:step`);
        closeStep(block.startedAt ?? block.endedAt);
        call(block, !toolEnded(block.status) && index >= live);
        if (block.endedAt !== undefined) {
          handedAt = Math.max(handedAt ?? 0, block.endedAt);
        }
        break;
      }
      case "message":
        if (block.outgoing) {
          // Sending it is the model's doing, as a call is.
          if (block.opensStep && !open) openStep(`${block.id}:step`);
          closeStep(block.at);
          started(block.recipientSessionId, block.messageId, block.at, sent);
        } else if (block.at !== undefined) {
          // A report is handed to the model, like the result of a call.
          handedAt = Math.max(handedAt ?? 0, block.at);
        }
        break;
      case "turn":
        closeStep(block.at);
        if (block.at !== undefined) {
          tally.you.seen.out++;
          tally.you.seen.answers++;
        }
        entries.push({
          id: block.id,
          station: "you",
          label: "answer ready",
          status: "ok",
          times: 1,
          // The whole turn, from the prompt being sent, as under the answer.
          tookMs: block.at === undefined ? null : block.durationMs,
          plain: false,
          at: block.at,
        });
        break;
    }
  });

  // A subagent no message of the chat leads to still counts, with its work.
  for (const child of subagents) {
    if (drawn.has(child)) continue;
    subagent(child, undefined);
    work(child, child.blocks);
  }

  if (running) {
    const waiting = STATIONS.some(({ id }) => tally[id].open > 0);
    // With every call back and nothing said yet, the model has its next step.
    if (!open && !waiting) openStep("step:next");
    // The narrowing above does not see the assignment inside openStep.
    const working = open as Entry | null;
    if (working) {
      working.status = "run";
      working.plain = false;
    }
  }

  const seen = inOrderSeen(entries);
  const going = seen.filter((entry) => entry.status === "run");
  const top = going[going.length - 1];
  return {
    tally,
    rows: fold(seen),
    now: top
      ? `${LABEL[top.station]}: ${top.label}${going.length > 1 ? ` and ${going.length - 1} more` : ""}`
      : null,
    route: routeOf(seen),
    total:
      entries.length +
      STATIONS.reduce((sum, { id }) => sum + tally[id].seen.back, 0),
  };
}
