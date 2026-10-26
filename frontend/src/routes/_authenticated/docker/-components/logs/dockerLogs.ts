type DockerLogLevel =
  | "debug"
  | "error"
  | "fatal"
  | "info"
  | "trace"
  | "unknown"
  | "warn";

export interface DockerLogEntry {
  /** Short container id. */
  id: string;
  level: DockerLogLevel;
  /** The line as the container wrote it, ANSI escapes included. */
  line: string;
  name: string;
  /** Arrival order; the row id and the tie-breaker for equal timestamps. */
  seq: number;
  stderr: boolean;
  /** `line` without ANSI escapes: what search, export and level detection see. */
  text: string;
  /** Docker RFC3339Nano timestamp, or "" when the line had none. */
  ts: string;
}

type DockerLogPosition = "end" | "middle" | "single" | "start";

/** A table row: an entry plus its place in a multi-line group. */
export interface DockerLogRow extends DockerLogEntry {
  position: DockerLogPosition;
}

interface DockerLogSegment {
  bold: boolean;
  /** One of the eight ANSI colour names, or "" for the default colour. */
  color: string;
  text: string;
}

export const DOCKER_LOG_BUFFER_LIMIT = 5000;

const TIME_FORMATTER = new Intl.DateTimeFormat(undefined, {
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hour12: false,
});

// oxlint-disable-next-line no-control-regex -- the escape byte is the point.
const ANSI_SEQUENCE = /\u001b\[([0-9;]*)([a-zA-Z])/g;
const ANSI_COLORS = [
  "black",
  "red",
  "green",
  "yellow",
  "blue",
  "magenta",
  "cyan",
  "white",
];

/**
 * Splits a line into runs of one SGR style. Only the eight named colours and
 * bold are kept; bright colours share the base name and 256/true colours are
 * skipped. Non-SGR sequences are dropped.
 */
export function parseDockerLogAnsi(line: string): DockerLogSegment[] {
  if (!line.includes("\u001b")) return [{ bold: false, color: "", text: line }];
  const segments: DockerLogSegment[] = [];
  let bold = false;
  let color = "";
  let last = 0;
  const push = (text: string) => {
    if (text) segments.push({ bold, color, text });
  };
  for (const match of line.matchAll(ANSI_SEQUENCE)) {
    push(line.slice(last, match.index));
    last = match.index + match[0].length;
    if (match[2] !== "m") continue;
    const codes = match[1] ? match[1].split(";").map(Number) : [0];
    for (let i = 0; i < codes.length; i++) {
      const code = codes[i];
      if (code === 0) {
        bold = false;
        color = "";
      } else if (code === 1) bold = true;
      else if (code === 22) bold = false;
      else if (code === 39) color = "";
      else if (code >= 30 && code <= 37) color = ANSI_COLORS[code - 30];
      else if (code >= 90 && code <= 97) color = ANSI_COLORS[code - 90];
      else if (code === 38 || code === 48) {
        i += codes[i + 1] === 5 ? 2 : codes[i + 1] === 2 ? 4 : 0;
      }
    }
  }
  push(line.slice(last));
  return segments;
}

/** One Channel frame holds one or more NDJSON records; malformed lines are skipped. */
export function parseDockerLogFrame(
  text: string,
  firstSeq: number,
): DockerLogEntry[] {
  const entries: DockerLogEntry[] = [];
  for (const raw of text.split("\n")) {
    if (!raw) continue;
    let record: unknown;
    try {
      record = JSON.parse(raw);
    } catch {
      continue;
    }
    if (typeof record !== "object" || record === null) continue;
    const { id, name, ts, line, stderr } = record as Record<string, unknown>;
    if (
      typeof id !== "string" ||
      typeof name !== "string" ||
      typeof line !== "string"
    ) {
      continue;
    }
    const text = line.replace(ANSI_SEQUENCE, "");
    entries.push({
      id,
      level: dockerLogLevel(text),
      line,
      name,
      seq: firstSeq + entries.length,
      stderr: stderr === true,
      text,
      ts: typeof ts === "string" ? ts : "",
    });
  }
  return entries;
}

// Docker timestamps are UTC with nine fractional digits, so they order as strings.
function compareDockerLogEntries(a: DockerLogEntry, b: DockerLogEntry): number {
  if (a.ts && b.ts && a.ts !== b.ts) return a.ts < b.ts ? -1 : 1;
  return a.seq - b.seq;
}

/**
 * Appends a flushed batch in timestamp order and drops the oldest entries past
 * `limit`. The backlog of several containers arrives interleaved, so a batch
 * that starts before the newest buffered line re-sorts the whole buffer; live
 * batches just append.
 */
export function mergeDockerLogEntries(
  current: DockerLogEntry[],
  batch: DockerLogEntry[],
  limit = DOCKER_LOG_BUFFER_LIMIT,
): DockerLogEntry[] {
  if (batch.length === 0) return current;
  const sorted = batch.toSorted(compareDockerLogEntries);
  const newest = current.at(-1);
  // ponytail: O(n log n) resort on out-of-order batches; a merge pass if the
  // 5000-entry cap ever grows.
  const next =
    newest && compareDockerLogEntries(sorted[0], newest) < 0
      ? [...current, ...sorted].sort(compareDockerLogEntries)
      : current.concat(sorted);
  return next.length > limit ? next.slice(next.length - limit) : next;
}

const LEVEL_ALIASES: Record<string, DockerLogLevel> = {
  crit: "fatal",
  critical: "fatal",
  dbg: "debug",
  dbug: "debug",
  debug: "debug",
  err: "error",
  error: "error",
  fail: "error",
  fatal: "fatal",
  inf: "info",
  info: "info",
  information: "info",
  note: "info",
  notice: "info",
  panic: "fatal",
  severe: "fatal",
  trace: "trace",
  trce: "trace",
  verbose: "trace",
  warn: "warn",
  warning: "warn",
  wrn: "warn",
};
const LETTER_LEVELS: Record<string, DockerLogLevel> = {
  D: "debug",
  E: "error",
  F: "fatal",
  I: "info",
  T: "trace",
  V: "trace",
  W: "warn",
};
// Longest first so "warning" wins over "warn".
const ALIASES = Object.keys(LEVEL_ALIASES)
  .sort((a, b) => b.length - a.length)
  .join("|");
const UPPER_ALIASES = ALIASES.toUpperCase();
const LEADING_TIMESTAMP =
  /^\d{4}[-/]\d{2}[-/]\d{2}(?:[T ]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?Z?)?\s+/;
// Most confident shape first; the first match wins. Trimmed from Dozzle's
// level guesser (internal/container/logparse/level_guesser.go).
const LEVEL_PATTERNS: RegExp[] = [
  // ERROR: ..., "err ..." at the start of the line.
  new RegExp(`^(${ALIASES})(?:[^a-z0-9]|$)`, "i"),
  // klog/glog header: E0806 14:55:55.980915 ...
  /^([EWIDFTV])\d{4} \d{2}:\d{2}:\d{2}\.\d{6}/,
  // Serilog header: [09:58:00 INF] ...
  new RegExp(`^\\[[^\\]]*\\d\\s+(${ALIASES})\\s*\\]`, "i"),
  // [ERROR], [E]
  new RegExp(`\\[ ?(${ALIASES}) ?\\]`, "i"),
  /\[([EWIDFTV])\]/,
  // JSON loggers: "level":"warn", "@l":"Error", "severity":"INFO"
  new RegExp(`"(?:@l|level|log\\.level|severity)"\\s*:\\s*"(${ALIASES})"`, "i"),
  // ::INFO::, Zigbee2MQTT:info, "[1]: INFO" (a gap only before upper case).
  new RegExp(`:(${ALIASES})(?:[^a-z0-9]|$)`, "i"),
  new RegExp(`:\\s+(${UPPER_ALIASES})(?:\\s|$)`),
  // Bare upper-case token or a separator-suffixed word inside the line.
  new RegExp(`\\s(${UPPER_ALIASES})(?::?\\s)`),
  new RegExp(` (${ALIASES})[/|:-]`, "i"),
];

/** Best-effort log level of one line; "unknown" when no level word is found. */
export function dockerLogLevel(line: string): DockerLogLevel {
  const text = line.replace(LEADING_TIMESTAMP, "");
  for (const pattern of LEVEL_PATTERNS) {
    const word = pattern.exec(text)?.[1];
    if (!word) continue;
    return word.length === 1
      ? LETTER_LEVELS[word]
      : LEVEL_ALIASES[word.toLowerCase()];
  }
  return "unknown";
}

// Lines closer than this belong to one entry (Dozzle's MaxGroupTimeDelta).
const GROUP_WINDOW_MS = 50;

/**
 * Folds a levelled line and the same-container lines that follow within
 * GROUP_WINDOW_MS, carrying no level or the same one, into one entry drawn as
 * a rail. Continuation lines inherit the group level.
 */
export function groupDockerLogRows(entries: DockerLogEntry[]): DockerLogRow[] {
  const times = entries.map((entry) => Date.parse(entry.ts));
  const rows: DockerLogRow[] = [];
  let i = 0;
  while (i < entries.length) {
    const first = entries[i];
    let end = i;
    // shortcut: no line cap on a group; add Dozzle's 1000-line cap if one grows unbounded.
    while (
      first.level !== "unknown" &&
      end + 1 < entries.length &&
      entries[end + 1].name === first.name &&
      (entries[end + 1].level === "unknown" ||
        entries[end + 1].level === first.level) &&
      Math.abs(times[end + 1] - times[end]) < GROUP_WINDOW_MS
    ) {
      end++;
    }
    if (end === i) {
      rows.push({ ...first, position: "single" });
    } else {
      for (let j = i; j <= end; j++) {
        rows.push({
          ...entries[j],
          level: first.level,
          position: j === i ? "start" : j === end ? "end" : "middle",
        });
      }
    }
    i = end + 1;
  }
  return rows;
}

export function formatDockerLogTime(ts: string): string {
  if (!ts) return "";
  const date = new Date(ts);
  return Number.isNaN(date.getTime()) ? ts : TIME_FORMATTER.format(date);
}

/**
 * Stages a parsed frame for the next flush, keeping only the newest `limit`
 * entries so a paused animation frame (background tab) cannot grow the queue
 * without bound.
 */
export function stageDockerLogEntries(
  pending: DockerLogEntry[],
  batch: DockerLogEntry[],
  limit = DOCKER_LOG_BUFFER_LIMIT,
): void {
  for (const entry of batch) pending.push(entry);
  if (pending.length > limit) pending.splice(0, pending.length - limit);
}
