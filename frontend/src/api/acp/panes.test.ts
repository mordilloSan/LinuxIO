import { afterEach, describe, expect, it, vi } from "vitest";

import {
  paneAttachAnswer,
  paneCapabilities,
  panePanesAnswer,
  panePushedInvalidate,
  panePushedLog,
  panePushedRoster,
  paneRenderAnswer,
  paneSessionId,
} from "@/test/assistantPaneFixture";

import { createPaneClient, type PaneView } from "./panes";
import type { PaneRequest } from "./transport";

interface Sent {
  conn: number;
  id: string;
  request: PaneRequest;
}

function harness(timeoutMs?: number) {
  const sent: Sent[] = [];
  const changes: PaneView[][] = [];
  const client = createPaneClient({
    onChange: (views) => changes.push(views),
    timeoutMs,
  });
  const send = (conn: number, id: string, request: PaneRequest) => {
    sent.push({ conn, id, request });
  };
  const message = (conn: number, value: unknown) =>
    client.receive({ conn, message: value }, send);
  const h = {
    client,
    sent,
    changes,
    message,
    open: (conn: number) => client.receive({ conn, event: "open" }, send),
    close: (conn: number) => client.receive({ conn, event: "close" }, send),
    ready: (conn: number, capabilities = paneCapabilities) =>
      message(conn, { type: "relay_ready", capabilities }),
    answer: (conn: number, id: string, response: unknown) =>
      message(conn, { type: "pane_response", id, ok: true, response }),
    refuse: (conn: number, id: string) =>
      message(conn, {
        type: "pane_response",
        id,
        ok: false,
        error: "Unknown pane",
      }),
    last: () => sent[sent.length - 1],
    views: () => changes[changes.length - 1] ?? [],
  };
  return h;
}

type Harness = ReturnType<typeof harness>;

// The spike's order: attach, the session's first ui_* message, the roster,
// then the render of the shown pane.
function attach(h: Harness, conn = 1, log: object = panePushedLog) {
  h.open(conn);
  h.ready(conn);
  const attachRequest = h.last();
  h.answer(conn, attachRequest.id, paneAttachAnswer);
  const panesRequest = h.last();
  h.message(conn, log);
  h.answer(conn, panesRequest.id, panePanesAnswer);
  const renderRequest = h.last();
  h.answer(conn, renderRequest.id, paneRenderAnswer);
  return { attachRequest, panesRequest, renderRequest };
}

const shownView = (conn = 1, sessionId = paneSessionId): PaneView => ({
  conn,
  sessionId,
  id: "lot-map",
  title: "Work map",
  plugin: "lot-map",
  tree: paneRenderAnswer.tree,
});

afterEach(() => {
  vi.useRealTimers();
});

describe("createPaneClient", () => {
  it("attaches, reads the roster and renders the shown pane", () => {
    const h = harness();
    const { attachRequest, panesRequest, renderRequest } = attach(h);
    const clientId = attachRequest.request.client_id;
    expect(clientId).toMatch(/^linuxio-[0-9a-f]{16}$/);
    expect(attachRequest).toEqual({
      conn: 1,
      id: expect.any(String),
      request: {
        subtype: "ui_attach",
        surface: "vscode",
        client_id: clientId,
        viewport: { columns: 60, rows: 20, isFullscreen: false },
      },
    });
    expect(panesRequest.request).toEqual({
      subtype: "ui_panes",
      client_id: clientId,
    });
    expect(renderRequest.request).toEqual({
      subtype: "ui_render",
      surface: "vscode",
      client_id: clientId,
      component: "Pane",
      instance_id: "lot-map",
      props: {},
    });
    expect(new Set(h.sent.map((s) => s.id)).size).toBe(3);
    expect(h.views()).toEqual([shownView()]);
  });

  it("sends nothing before relay_ready and nothing without ui_surface_v1", () => {
    const h = harness();
    h.open(1);
    h.message(1, panePushedLog);
    expect(h.sent).toEqual([]);
    h.ready(1, ["prompts_screened_v1"]);
    expect(h.sent).toEqual([]);
    expect(h.views()).toEqual([]);
  });

  it("offers the pane only once a ui_* message names its session", () => {
    const h = harness();
    h.open(1);
    h.ready(1);
    h.answer(1, h.last().id, paneAttachAnswer);
    h.answer(1, h.last().id, panePanesAnswer);
    h.answer(1, h.last().id, paneRenderAnswer);
    expect(h.views()).toEqual([]);
    h.message(1, panePushedRoster);
    expect(h.views()).toEqual([shownView()]);
  });

  it("renders again on ui_invalidate, one in flight and at most one queued", () => {
    const h = harness();
    attach(h);
    const before = h.sent.length;
    for (let i = 0; i < 4; i++) h.message(1, panePushedInvalidate);
    expect(h.sent).toHaveLength(before + 1);
    const first = h.last();
    expect(first.request).toMatchObject({
      subtype: "ui_render",
      instance_id: "lot-map",
    });
    const redrawn = {
      tree: { type: "Text", props: {}, children: ["Running"] },
    };
    h.answer(1, first.id, redrawn);
    // The queued render goes out once the first answers.
    expect(h.sent).toHaveLength(before + 2);
    expect(h.views()[0].tree).toEqual(redrawn.tree);
    h.answer(1, h.last().id, paneRenderAnswer);
    expect(h.sent).toHaveLength(before + 2);
    expect(h.views()).toEqual([shownView()]);
  });

  it("ignores invalidations of other panes, components and surfaces", () => {
    const h = harness();
    attach(h);
    const before = h.sent.length;
    const invalidate = (instance: object) =>
      h.message(1, { ...panePushedInvalidate, instances: [instance] });
    invalidate({ surface: "vscode", component: "Pane", instance_id: "other" });
    invalidate({ surface: "vscode", component: "Tab", instance_id: "lot-map" });
    invalidate({
      surface: "desktop",
      component: "Pane",
      instance_id: "lot-map",
    });
    expect(h.sent).toHaveLength(before);
  });

  it("takes a pushed roster: a newly shown pane is rendered, a removed one hidden", () => {
    const h = harness();
    attach(h);
    h.message(1, {
      ...panePushedRoster,
      panes: [
        ...panePushedRoster.panes,
        { id: "notes", title: "Notes", plugin: "notes" },
      ],
      shown_id: "notes",
    });
    expect(h.views()).toEqual([]);
    expect(h.last().request).toMatchObject({
      subtype: "ui_render",
      instance_id: "notes",
    });
    const tree = { type: "Markdown", props: { text: "# Notes" } };
    h.answer(1, h.last().id, { tree });
    expect(h.views()).toEqual([
      { ...shownView(), id: "notes", title: "Notes", plugin: "notes", tree },
    ]);
    h.message(1, { ...panePushedRoster, panes: [], shown_id: null });
    expect(h.views()).toEqual([]);
  });

  it("does not render again for a repeated roster", () => {
    const h = harness();
    attach(h);
    const before = h.sent.length;
    h.message(1, panePushedRoster);
    h.message(1, panePushedRoster);
    expect(h.sent).toHaveLength(before);
    expect(h.views()).toEqual([shownView()]);
  });

  it("keeps the drawing when one render is refused and asks again on the next invalidation", () => {
    const h = harness();
    attach(h);
    h.message(1, panePushedInvalidate);
    h.refuse(1, h.last().id);
    expect(h.views()).toEqual([shownView()]);
    const before = h.sent.length;
    h.message(1, panePushedInvalidate);
    expect(h.sent).toHaveLength(before + 1);
    expect(h.last().request.subtype).toBe("ui_render");
  });

  it("hides the pane after three renders fail in a row and stops asking", () => {
    const h = harness();
    attach(h);
    for (let i = 0; i < 2; i++) {
      h.message(1, panePushedInvalidate);
      h.refuse(1, h.last().id);
      expect(h.views()).toEqual([shownView()]);
    }
    h.message(1, panePushedInvalidate);
    h.refuse(1, h.last().id);
    expect(h.views()).toEqual([]);
    const before = h.sent.length;
    h.message(1, panePushedInvalidate);
    h.message(1, panePushedRoster);
    expect(h.sent).toHaveLength(before);
    expect(h.views()).toEqual([]);
  });

  it("starts counting again after a render that worked", () => {
    const h = harness();
    attach(h);
    for (let i = 0; i < 5; i++) {
      h.message(1, panePushedInvalidate);
      h.refuse(1, h.last().id);
      h.message(1, panePushedInvalidate);
      h.answer(1, h.last().id, paneRenderAnswer);
    }
    expect(h.views()).toEqual([shownView()]);
  });

  it("hides the pane when the attach is refused", () => {
    const h = harness();
    h.open(1);
    h.ready(1);
    h.refuse(1, h.last().id);
    expect(h.sent).toHaveLength(1);
    expect(h.views()).toEqual([]);
  });

  it.each([
    ["a message that is not an object", "garbage"],
    ["a message without a type", { subtype: "ui_panes" }],
    ["a roster whose panes are not a list", { ...panePushedRoster, panes: 3 }],
    [
      "a roster entry without a title",
      { ...panePushedRoster, panes: [{ id: "lot-map" }] },
    ],
    [
      "an invalidation without instances",
      { ...panePushedInvalidate, instances: null },
    ],
    ["an answer without an id", { type: "pane_response", ok: true }],
  ])("hides the pane on %s", (_name, bad) => {
    const h = harness();
    attach(h);
    h.message(1, bad);
    expect(h.views()).toEqual([]);
  });

  it("hides the pane when a render answer has no tree", () => {
    const h = harness();
    attach(h);
    h.message(1, panePushedInvalidate);
    h.answer(1, h.last().id, { tree: "<b>hi</b>" });
    expect(h.views()).toEqual([]);
  });

  it("treats an unanswered render as a failed one and ignores the late answer", () => {
    vi.useFakeTimers();
    const h = harness(1000);
    attach(h);
    h.message(1, panePushedInvalidate);
    const late = h.last();
    vi.advanceTimersByTime(1000);
    expect(h.views()).toEqual([shownView()]);
    const before = h.sent.length;
    h.answer(1, late.id, { tree: "late" });
    expect(h.sent).toHaveLength(before);
    expect(h.views()).toEqual([shownView()]);
    // The next invalidation asks again; nothing is stuck in flight.
    h.message(1, panePushedInvalidate);
    expect(h.sent).toHaveLength(before + 1);
  });

  it("hides the pane when the attach goes unanswered", () => {
    vi.useFakeTimers();
    const h = harness(1000);
    h.open(1);
    h.ready(1);
    vi.advanceTimersByTime(1000);
    const before = h.sent.length;
    h.answer(1, h.last().id, paneAttachAnswer);
    expect(h.sent).toHaveLength(before);
    expect(h.views()).toEqual([]);
  });

  it("does not time out answered requests", () => {
    vi.useFakeTimers();
    const h = harness(1000);
    attach(h);
    vi.advanceTimersByTime(5000);
    expect(h.views()).toEqual([shownView()]);
  });

  it("keeps each Claude Code process apart and forgets a closed one", () => {
    const h = harness();
    attach(h, 1);
    attach(h, 2, { ...panePushedLog, session_id: "side-session" });
    expect(h.views()).toEqual([shownView(1), shownView(2, "side-session")]);
    h.close(1);
    expect(h.views()).toEqual([shownView(2, "side-session")]);
  });

  it("keeps a failed connection from hiding another", () => {
    const h = harness();
    attach(h, 1);
    attach(h, 2, { ...panePushedLog, session_id: "other" });
    h.message(2, "garbage");
    expect(h.views()).toEqual([shownView(1)]);
  });

  it("forgets everything on reset and ignores what was in flight", () => {
    vi.useFakeTimers();
    const h = harness(1000);
    attach(h);
    h.message(1, panePushedInvalidate);
    const inFlight = h.last();
    const changes = h.changes.length;
    const sent = h.sent.length;
    // The store clears its own copy; reset reports nothing.
    h.client.reset();
    vi.advanceTimersByTime(5000);
    h.answer(1, inFlight.id, paneRenderAnswer);
    h.message(1, panePushedInvalidate);
    expect(h.changes).toHaveLength(changes);
    expect(h.sent).toHaveLength(sent);
    // A connection opened after the reset starts over.
    attach(h, 1);
    expect(h.sent).toHaveLength(sent + 3);
    expect(h.views()).toEqual([shownView()]);
  });

  it("does not report a change when nothing visible changed", () => {
    const h = harness();
    attach(h);
    const changes = h.changes.length;
    h.message(1, panePushedLog);
    h.message(1, { type: "system", subtype: "status", session_id: "x" });
    h.message(1, { type: "unknown_relay_line" });
    expect(h.changes).toHaveLength(changes);
  });
});
