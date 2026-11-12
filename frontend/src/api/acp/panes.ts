import type { PaneLine, PaneRequest, PaneSend } from "./transport";

/**
 * A node of a plugin's render tree as Claude Code sends it. It is untrusted:
 * draw it only through the pane renderer, which checks every field again.
 */
export interface PaneNode {
  type: string;
  props?: unknown;
  children?: unknown;
}

/** The pane one Claude Code process shows, with its latest render. */
export interface PaneView {
  /** The bridge's number for that process's pane connection. */
  conn: number;
  /** Claude Code's session id, which is the chat's ACP session id. */
  sessionId: string;
  id: string;
  title: string;
  plugin: string;
  tree: PaneNode;
}

export interface PaneClient {
  /** Handles one pane line from the bridge; `send` answers on the same stream. */
  receive(line: PaneLine, send: PaneSend): void;
  /** Forgets every connection, e.g. when the stream that carried them ended. */
  reset(): void;
}

interface PaneClientOptions {
  /** Called with every pane that can be drawn whenever that list changes. */
  onChange: (views: PaneView[]) => void;
  /** How long a request may go unanswered before its pane is hidden. */
  timeoutMs?: number;
}

/** Claude Code answers pane requests between turns' messages; a healthy one takes milliseconds. */
export const PANE_REQUEST_TIMEOUT_MS = 15_000;

const SURFACE_CAPABILITY = "ui_surface_v1";
// Claude Code accepts "desktop", "mobile" and "vscode"; the VS Code surface
// matches what LinuxIO draws.
const SURFACE = "vscode";
const VIEWPORT = { columns: 60, rows: 20, isFullscreen: false };
// A plugin's render can fail once (its own error, a busy turn) without the
// pane being lost: the last drawing stays until this many fail in a row.
const MAX_RENDER_FAILURES = 3;

interface RosterEntry {
  id: string;
  title: string;
  plugin: string;
}

type Waiting =
  | { kind: "attach" }
  | { kind: "panes" }
  | { kind: "render"; paneId: string };

interface Connection {
  send: PaneSend;
  /**
   * waiting: for relay_ready; attaching: ui_attach and ui_panes in flight;
   * attached: renders flow; failed: hidden until the connection closes.
   */
  phase: "waiting" | "attaching" | "attached" | "failed";
  sessionId: string | null;
  roster: RosterEntry[];
  shownId: string | null;
  tree: { paneId: string; node: PaneNode } | null;
  /** Panes with a render in flight. */
  rendering: Set<string>;
  /** Panes invalidated while their render was in flight: one more is due. */
  queued: Set<string>;
  /** Renders refused or unanswered in a row. */
  renderFailures: number;
  pending: Map<
    string,
    { waiting: Waiting; timer: ReturnType<typeof setTimeout> }
  >;
}

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

// client_id allows 1-64 of letters, digits, ".", "_" and "-". getRandomValues
// works on plain-http installs, where randomUUID does not exist.
function newClientId() {
  const bytes = crypto.getRandomValues(new Uint8Array(8));
  return `linuxio-${Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("")}`;
}

/** Reads a roster (`ui_panes` answer or push); null when it does not parse. */
function parseRoster(
  value: Record<string, unknown>,
): { roster: RosterEntry[]; shownId: string | null } | null {
  const { panes, shown_id: shownId } = value;
  if (!Array.isArray(panes)) return null;
  if (shownId !== null && shownId !== undefined && typeof shownId !== "string")
    return null;
  const roster: RosterEntry[] = [];
  for (const pane of panes) {
    if (
      !isRecord(pane) ||
      typeof pane.id !== "string" ||
      typeof pane.title !== "string" ||
      (pane.plugin !== undefined && typeof pane.plugin !== "string")
    ) {
      return null;
    }
    roster.push({ id: pane.id, title: pane.title, plugin: pane.plugin ?? "" });
  }
  return { roster, shownId: shownId ?? null };
}

const isPaneNode = (value: unknown): value is PaneNode =>
  isRecord(value) && typeof value.type === "string";

const sameViews = (a: PaneView[], b: PaneView[]) =>
  a.length === b.length &&
  a.every(
    (view, i) =>
      view.conn === b[i].conn &&
      view.sessionId === b[i].sessionId &&
      view.id === b[i].id &&
      view.title === b[i].title &&
      view.plugin === b[i].plugin &&
      view.tree === b[i].tree,
  );

/**
 * Speaks Claude Code's pane protocol on each pane connection the bridge
 * carries: attach, read the roster, render the shown pane, and render it
 * again when Claude Code invalidates it. A refused attach or roster, a message
 * that does not parse, or three renders failing in a row hides that
 * connection's pane until it closes; nothing here ever reaches the chat.
 */
export function createPaneClient({
  onChange,
  timeoutMs = PANE_REQUEST_TIMEOUT_MS,
}: PaneClientOptions): PaneClient {
  const clientId = newClientId();
  const connections = new Map<number, Connection>();
  let nextRequest = 0;
  let views: PaneView[] = [];

  const emit = () => {
    const next: PaneView[] = [];
    for (const [conn, c] of [...connections].sort(([a], [b]) => a - b)) {
      const shown = c.roster.find((pane) => pane.id === c.shownId);
      if (
        c.phase !== "attached" ||
        !c.sessionId ||
        !shown ||
        c.tree?.paneId !== shown.id
      ) {
        continue;
      }
      next.push({
        conn,
        sessionId: c.sessionId,
        id: shown.id,
        title: shown.title,
        plugin: shown.plugin,
        tree: c.tree.node,
      });
    }
    if (sameViews(views, next)) return;
    views = next;
    onChange(views);
  };

  const stopWaiting = (c: Connection) => {
    for (const { timer } of c.pending.values()) clearTimeout(timer);
    c.pending.clear();
  };

  const fail = (c: Connection) => {
    stopWaiting(c);
    c.phase = "failed";
    c.tree = null;
    c.rendering.clear();
    c.queued.clear();
  };

  const request = (
    conn: number,
    c: Connection,
    waiting: Waiting,
    body: PaneRequest,
  ) => {
    const id = `p${++nextRequest}`;
    const timer = setTimeout(() => {
      if (connections.get(conn) !== c || !c.pending.has(id)) return;
      c.pending.delete(id);
      if (waiting.kind === "render") renderFailed(conn, c, waiting.paneId);
      else fail(c);
      emit();
    }, timeoutMs);
    c.pending.set(id, { waiting, timer });
    c.send(conn, id, body);
  };

  // Keeps what is drawn: the next invalidation, or the one that queued
  // meanwhile, asks again.
  function renderFailed(conn: number, c: Connection, paneId: string) {
    c.rendering.delete(paneId);
    const again = c.queued.delete(paneId);
    if (++c.renderFailures >= MAX_RENDER_FAILURES) {
      fail(c);
      return;
    }
    if (again && paneId === c.shownId) render(conn, c, paneId);
  }

  function render(conn: number, c: Connection, paneId: string) {
    if (c.rendering.has(paneId)) {
      c.queued.add(paneId);
      return;
    }
    c.rendering.add(paneId);
    request(
      conn,
      c,
      { kind: "render", paneId },
      {
        subtype: "ui_render",
        surface: SURFACE,
        client_id: clientId,
        component: "Pane",
        instance_id: paneId,
        props: {},
      },
    );
  }

  // A new roster may show another pane: drop the old tree and draw the new one.
  const applyRoster = (
    conn: number,
    c: Connection,
    parsed: { roster: RosterEntry[]; shownId: string | null },
  ) => {
    c.roster = parsed.roster;
    c.shownId = parsed.roster.some((pane) => pane.id === parsed.shownId)
      ? parsed.shownId
      : null;
    if (c.tree && c.tree.paneId !== c.shownId) c.tree = null;
    if (c.phase === "attached" && c.shownId && !c.tree) {
      render(conn, c, c.shownId);
    }
  };

  const answered = (
    conn: number,
    c: Connection,
    waiting: Waiting,
    response: unknown,
  ): boolean => {
    switch (waiting.kind) {
      case "attach":
        request(
          conn,
          c,
          { kind: "panes" },
          {
            subtype: "ui_panes",
            client_id: clientId,
          },
        );
        return true;
      case "panes": {
        const parsed = isRecord(response) ? parseRoster(response) : null;
        if (!parsed) return false;
        c.phase = "attached";
        applyRoster(conn, c, parsed);
        return true;
      }
      case "render": {
        const { paneId } = waiting;
        c.rendering.delete(paneId);
        const tree = isRecord(response) ? response.tree : undefined;
        if (!isPaneNode(tree)) return false;
        c.renderFailures = 0;
        if (paneId === c.shownId) c.tree = { paneId, node: tree };
        if (c.queued.delete(paneId) && paneId === c.shownId) {
          render(conn, c, paneId);
        }
        return true;
      }
    }
  };

  // Returns false when the message does not parse.
  const handleMessage = (
    conn: number,
    c: Connection,
    message: unknown,
  ): boolean => {
    if (!isRecord(message) || typeof message.type !== "string") return false;
    switch (message.type) {
      case "relay_ready": {
        if (c.phase !== "waiting") return true;
        const { capabilities } = message;
        if (!Array.isArray(capabilities)) return false;
        if (!capabilities.includes(SURFACE_CAPABILITY)) {
          // This Claude Code has no panes; nothing to hide or show.
          fail(c);
          return true;
        }
        c.phase = "attaching";
        request(
          conn,
          c,
          { kind: "attach" },
          {
            subtype: "ui_attach",
            surface: SURFACE,
            client_id: clientId,
            viewport: VIEWPORT,
          },
        );
        return true;
      }
      case "pane_response": {
        if (typeof message.id !== "string") return false;
        const entry = c.pending.get(message.id);
        // A late answer to a request that timed out, or someone else's.
        if (!entry) return true;
        clearTimeout(entry.timer);
        c.pending.delete(message.id);
        if (message.ok !== true) {
          if (entry.waiting.kind !== "render") return false;
          renderFailed(conn, c, entry.waiting.paneId);
          return true;
        }
        return answered(conn, c, entry.waiting, message.response);
      }
      case "system": {
        const { subtype } = message;
        if (typeof subtype !== "string" || !subtype.startsWith("ui_"))
          return true;
        if (typeof message.session_id === "string" && message.session_id) {
          c.sessionId = message.session_id;
        }
        if (subtype === "ui_panes") {
          const parsed = parseRoster(message);
          if (!parsed) return false;
          applyRoster(conn, c, parsed);
        } else if (subtype === "ui_invalidate") {
          const { instances } = message;
          if (!Array.isArray(instances)) return false;
          const shownId = c.shownId;
          const named = instances.some(
            (instance) =>
              isRecord(instance) &&
              instance.component === "Pane" &&
              instance.instance_id === shownId &&
              (instance.surface === undefined || instance.surface === SURFACE),
          );
          if (c.phase === "attached" && shownId && named) {
            render(conn, c, shownId);
          }
        }
        return true;
      }
      default:
        return true;
    }
  };

  return {
    receive(line, send) {
      const { conn } = line;
      let c = connections.get(conn);
      if (line.event === "close") {
        if (c) {
          stopWaiting(c);
          connections.delete(conn);
          emit();
        }
        return;
      }
      if (!c) {
        c = {
          send,
          phase: "waiting",
          sessionId: null,
          roster: [],
          shownId: null,
          tree: null,
          rendering: new Set(),
          queued: new Set(),
          renderFailures: 0,
          pending: new Map(),
        };
        connections.set(conn, c);
      }
      if (line.event !== undefined || c.phase === "failed") return;
      if (!handleMessage(conn, c, line.message)) fail(c);
      emit();
    },
    reset() {
      for (const c of connections.values()) stopWaiting(c);
      connections.clear();
      views = [];
    },
  };
}
