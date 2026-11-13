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
import {
  stringField,
  type ToolBlock,
  toolEnded,
  type TranscriptBlock,
} from "../transcript/transcript";

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
   * animates: a chat that was loaded has none of it.
   */
  seen: { out: number; back: number; failed: number };
}

export interface WorkRow {
  id: string;
  station: StationId;
  label: string;
  status: "run" | "ok" | "err" | "switch";
  /** How many finished calls in a row at one station the row stands for. */
  times: number;
  /** How long it took, where that was seen happening. */
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
const ROUTE_STOPS = 7;

// Claude Code's tool names, which the adapter passes beside the ACP kind.
const NAMED: Record<string, StationId> = {
  Agent: "agents",
  Artifact: "network",
  AskUserQuestion: "you",
  Glob: "files",
  Grep: "files",
  SendMessage: "agents",
  SendUserFile: "you",
  Skill: "skills",
  Task: "agents",
  TaskStop: "agents",
  ToolSearch: "skills",
  WebFetch: "network",
  WebSearch: "network",
  Workflow: "agents",
  memory_recall: "memory",
};
const FILE_TOOLS = new Set(["Read", "Write", "Edit", "NotebookEdit"]);
const SHELL_TOOLS = new Set(["Bash", "PowerShell", "Monitor"]);

const FILE_KINDS = new Set(["read", "edit", "delete", "move", "search"]);

// Commands that work on files, reach the network, or do neither (and so do
// not decide where a command line belongs).
const FILE_WORDS = new Set(
  "awk basename cat cmp cp cut diff dirname du file find grep head jq ln ls mkdir mv realpath rg rm rmdir sed sort stat tail tee touch tr tree uniq wc".split(
    " ",
  ),
);
const NETWORK_WORDS = new Set(
  "curl dig ftp gh nc ncat nslookup ping rsync scp sftp ssh telnet wget".split(
    " ",
  ),
);
const NEUTRAL_WORDS = new Set(
  "[ cd echo export printf pwd set sleep test true which".split(" "),
);

// The agent's own notes.
const MEMORY_PATH = /\/\.claude\/projects\/[^/]+\/memory\//;
const BROWSER_SERVERS = new Set(["Claude_Browser", "claude-in-chrome"]);

/** The command names of a command line, one per piped or chained part. */
function commandNames(command: string): string[] {
  // Quoted text cannot split a command, so it is taken out first.
  const plain = command.replace(/'[^']*'|"(?:[^"\\]|\\.)*"/g, "_");
  return plain.split(/\|\||&&|[|;\n&]/).flatMap((part) => {
    const words = part.trim().split(/\s+/).filter(Boolean);
    while (
      words.length > 0 &&
      (/^\w+=/.test(words[0]) || words[0] === "sudo")
    ) {
      words.shift();
    }
    return words.length > 0 ? [words[0]] : [];
  });
}

/**
 * A command line belongs to the network when any part of it reaches out, to
 * files when every part only handles files, and to the shell otherwise.
 */
export function shellStation(command: string | null): StationId {
  if (command === null) return "shell";
  const names = commandNames(command).filter(
    (name) => !NEUTRAL_WORDS.has(name),
  );
  if (names.some((name) => NETWORK_WORDS.has(name))) return "network";
  return names.length > 0 && names.every((name) => FILE_WORDS.has(name))
    ? "files"
    : "shell";
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
        : shellStation(stringField(block.rawInput, "command"));
    }
    if (name in NAMED) return NAMED[name];
    const mcp = mcpOf(name);
    return mcp ? mcpStation(mcp.server) : "shell";
  }
  // An agent that sends no tool name is sorted by the ACP kind alone.
  if (FILE_KINDS.has(block.toolKind)) return fileStation(block.rawInput);
  switch (block.toolKind) {
    case "execute":
      return shellStation(stringField(block.rawInput, "command"));
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

const hostOf = (url: string) => {
  try {
    return new URL(url).host || url;
  } catch {
    return url;
  }
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
      described ?? commandNames(stringField(input, "command") ?? "")[0] ?? name
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
  const mcp = mcpOf(name);
  if (!mcp) return name;
  return mcpStation(mcp.server) === "network"
    ? `${mcp.server}: ${spaced(mcp.tool)}`
    : spaced(mcp.tool);
}

const LABEL = Object.fromEntries(
  STATIONS.map((station) => [station.id, station.label]),
) as Record<StationId, string>;

export const stationLabel = (id: StationId) => LABEL[id];

/** 113 ms, 13.4 s, 2 min 5 s. */
export function formatMs(ms: number): string {
  if (ms < 1000) return `${Math.max(1, Math.round(ms))} ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)} s`;
  return `${Math.floor(ms / 60_000)} min ${Math.round((ms % 60_000) / 1000)} s`;
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

/** A row of the feed: `· Files     disks/lsblk.go  +2  1.2 s`. */
export function rowText(row: WorkRow): string {
  const took =
    row.status === "run"
      ? "running"
      : row.tookMs !== null && row.tookMs > 0
        ? formatMs(row.tookMs)
        : "";
  return `${MARK[row.status]} ${stationLabel(row.station).padEnd(COLUMN)} ${row.label}${row.times > 1 ? `  +${row.times - 1}` : ""}${took === "" ? "" : `  ${took}`}`;
}

/** How long from one time to another, when both were seen. */
export const span = (from: number | undefined, to: number | undefined) =>
  from === undefined || to === undefined ? null : Math.max(0, to - from);

type Entry = WorkRow & {
  /** A finished call or step, which folds into the one before it. */
  plain: boolean;
  /** When it was seen happening, for the route. */
  at: number | undefined;
};

/** Finished calls in a row at one station read as one row, the newest. */
function fold(entries: readonly Entry[]): WorkRow[] {
  const rows: Entry[] = [];
  for (let i = entries.length - 1; i >= 0; i--) {
    const entry = entries[i];
    const last = rows[rows.length - 1];
    if (last?.plain && entry.plain && last.station === entry.station) {
      last.times++;
      if (entry.tookMs !== null) {
        last.tookMs = (last.tookMs ?? 0) + entry.tookMs;
      }
      continue;
    }
    if (rows.length === ROWS) break;
    rows.push({ ...entry });
  }
  return rows.map(({ plain: _plain, at: _at, ...row }) => row);
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
 */
export function summarize(
  blocks: readonly TranscriptBlock[],
  running: boolean,
): WorkSummary {
  const tally = Object.fromEntries(
    STATIONS.map((station) => [
      station.id,
      {
        count: 0,
        open: 0,
        faults: 0,
        seen: { out: 0, back: 0, failed: 0 },
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

  blocks.forEach((block, index) => {
    switch (block.kind) {
      case "user": {
        closeStep(undefined);
        step = 0;
        handedAt = block.at;
        for (const station of STATIONS) tally[station.id].faults = 0;
        tally.you.count++;
        if (block.at !== undefined) tally.you.seen.back++;
        entries.push({
          id: block.id,
          station: "you",
          label: "new message",
          status: "ok",
          times: 1,
          tookMs: null,
          plain: true,
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
            tookMs: null,
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
        if (block.opensStep && !open) openStep(`${block.id}:step`);
        closeStep(block.startedAt ?? block.endedAt);
        const id = stationOf(block);
        const station = tally[id];
        const ended = toolEnded(block.status);
        const failed = block.status === "failed";
        const runs = !ended && index >= live;
        station.count++;
        if (runs) station.open++;
        if (failed) station.faults++;
        if (block.startedAt !== undefined) station.seen.out++;
        if (block.endedAt !== undefined) {
          station.seen.back++;
          if (failed) station.seen.failed++;
          handedAt = Math.max(handedAt ?? 0, block.endedAt);
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
        });
        break;
      }
      case "turn":
        closeStep(block.at);
        if (block.at !== undefined) tally.you.seen.out++;
        entries.push({
          id: block.id,
          station: "you",
          label: "answer ready",
          status: "ok",
          times: 1,
          tookMs: null,
          plain: false,
          at: block.at,
        });
        break;
    }
  });

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

  const going = entries.filter((entry) => entry.status === "run");
  const top = going[going.length - 1];
  return {
    tally,
    rows: fold(entries),
    now: top
      ? `${LABEL[top.station]}: ${top.label}${going.length > 1 ? ` and ${going.length - 1} more` : ""}`
      : null,
    route: routeOf(entries),
    total:
      entries.length +
      STATIONS.reduce((sum, { id }) => sum + tally[id].seen.back, 0),
  };
}
