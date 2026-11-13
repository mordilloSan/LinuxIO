import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import AgentMap from "./AgentMap";
import type { AgentCall, AgentRun } from "./agents";

const calls: AgentCall[] = [
  {
    id: "c1",
    tool: "Read",
    detail: "filebrowser.md",
    code: true,
    note: "lines 421-465",
    full: "/docs/audit/filebrowser.md",
    status: "ok",
    tookMs: 120,
  },
  {
    id: "c2",
    tool: "Bash",
    detail: "Run the tests",
    code: false,
    note: null,
    full: "make test",
    status: "err",
    tookMs: null,
  },
];

const run = (over: Partial<AgentRun> = {}): AgentRun => ({
  id: "a1",
  label: "Re-check filebrowser audit items",
  type: "Explore",
  model: "haiku",
  startedBy: null,
  state: "finished",
  status: "ok",
  error: null,
  cwd: null,
  canStop: false,
  runningSince: null,
  ranMs: 78_000,
  parts: [
    {
      kind: "prompt",
      id: "p1",
      number: null,
      text: "Read the audit and check each item.",
    },
    {
      kind: "messages",
      id: "m1",
      texts: ["Reading the audit.", "Waiting for the test run to finish."],
    },
    { kind: "calls", id: "c1", calls },
    {
      kind: "report",
      id: "r1",
      number: null,
      text: "Two items are **fixed**.",
    },
  ],
  agents: [],
  ...over,
});

/** The view as the dock holds it: the open subagent is the dock's to keep. */
function Held({
  agents,
  onMain,
  onStop,
}: {
  agents: AgentRun[];
  onMain: () => void;
  onStop: (id: string) => void;
}) {
  const [chosenId, setChosenId] = useState<string | null>(null);
  return (
    <AgentMap
      agentLabel="Claude Code"
      agents={agents}
      chosenId={chosenId}
      model="Fable 5.2"
      onChoose={setChosenId}
      onMain={onMain}
      onStop={onStop}
      running={false}
      usage={{ used: 275_800, size: 1_000_000 }}
    />
  );
}

const draw = (agents: AgentRun[], onMain = vi.fn(), onStop = vi.fn()) =>
  render(<Held agents={agents} onMain={onMain} onStop={onStop} />);

const open = (name: RegExp) =>
  userEvent.click(screen.getByRole("button", { name }));

describe("AgentMap", () => {
  it("shows the chat's agent beside its subagents", () => {
    draw([
      run(),
      run({ id: "a2", label: "Write docs", state: "failed", status: "err" }),
      run({ id: "a3", label: "Wait", state: "waiting", status: "run" }),
    ]);
    expect(
      screen.getByText("3 agents · 1 running · select one for details"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /Claude Code/ }),
    ).toHaveTextContent("fable 5.2 · 275.8k tokens in context");
    expect(
      screen.getByRole("button", { name: /Re-check filebrowser/ }),
    ).toHaveTextContent("Explore · 1 min 18 s");
    expect(
      screen.getByRole("button", { name: /Write docs/ }),
    ).toHaveTextContent("failed");
    expect(screen.getByRole("button", { name: /Wait/ })).toHaveTextContent(
      "waiting for you",
    );
  });

  it("says so when the chat has no subagents", () => {
    draw([]);
    expect(
      screen.getByText("No subagents in this chat yet."),
    ).toBeInTheDocument();
  });

  it("opens a subagent to what it did, and goes back to it", async () => {
    draw([run()]);
    await open(/Re-check filebrowser/);

    const back = screen.getByRole("button", { name: "All agents" });
    expect(back).toHaveFocus();
    expect(
      screen.getByRole("heading", { name: "Re-check filebrowser audit items" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Finished · Explore · haiku")).toBeInTheDocument();
    expect(screen.getByText("Started by the main agent")).toBeInTheDocument();
    // No agent says what a subagent used.
    expect(
      screen.getByText("1 min 18 s · tokens not reported"),
    ).toBeInTheDocument();
    expect(screen.getByText("Prompt")).toBeInTheDocument();
    expect(screen.getByText("Messages (2)")).toBeInTheDocument();
    expect(screen.getByText("Tool calls (2)")).toBeInTheDocument();
    const [read, bash] = screen.getAllByRole("listitem");
    expect(read).toHaveTextContent("Readfilebrowser.md(lines 421-465)");
    expect(read).toHaveAttribute(
      "title",
      "/docs/audit/filebrowser.md · 120 ms",
    );
    expect(bash).toHaveTextContent("BashRun the testsfailed");
    expect(screen.getByText("Report").closest("details")).toHaveAttribute(
      "open",
    );
    expect(screen.getByText("fixed").tagName).toBe("STRONG");
    // It ended: nothing to stop.
    expect(screen.queryByRole("button", { name: "Stop" })).toBeNull();

    await userEvent.click(back);
    expect(
      screen.getByRole("button", { name: /Re-check filebrowser/ }),
    ).toHaveFocus();
  });

  it("shows a subagent that was messaged again in the order it happened", async () => {
    const one = (id: string, tool: string): AgentCall => ({
      ...calls[0],
      id,
      tool,
    });
    draw([
      run({
        parts: [
          { kind: "prompt", id: "p1", number: 1, text: "Read the OS release" },
          { kind: "calls", id: "c1", calls: [one("c1", "Read")] },
          { kind: "report", id: "r1", number: 1, text: "It is Debian." },
          { kind: "prompt", id: "p2", number: 2, text: "Run uptime" },
          { kind: "calls", id: "c2", calls: [one("c2", "Bash")] },
          { kind: "report", id: "r2", number: 2, text: "Up 3 days." },
        ],
      }),
    ]);
    await open(/Re-check filebrowser/);
    const order = [
      ...document.querySelectorAll(".agent-map--detail summary"),
    ].map((node) => node.textContent);
    expect(order).toEqual([
      "Prompt 1",
      "Tool calls (1)",
      "Report 1",
      "Prompt 2",
      "Tool calls (1)",
      "Report 2",
    ]);
  });

  it("says what it does not know about a subagent of a loaded chat", async () => {
    draw([run({ ranMs: null, parts: [] })]);
    await open(/Re-check filebrowser/);
    expect(screen.getByText("tokens not reported")).toBeInTheDocument();
    expect(screen.getByText("Nothing seen.")).toBeInTheDocument();
    expect(screen.queryByText(/Messages/)).toBeNull();
  });

  it("says how each subagent ended, why it failed and where it works", async () => {
    draw([
      run({
        state: "failed",
        status: "err",
        error: "Out of credit",
        cwd: "/srv/worktrees/a1",
      }),
      run({ id: "a2", label: "Stopped one", state: "stopped" }),
      run({ id: "a3", label: "Lost one", state: "disconnected" }),
      run({ id: "a4", label: "Ended one", state: "ended" }),
    ]);
    expect(
      screen.getByRole("button", { name: /Stopped one/ }),
    ).toHaveTextContent("stopped");
    expect(screen.getByRole("button", { name: /Lost one/ })).toHaveTextContent(
      "disconnected",
    );
    expect(screen.getByRole("button", { name: /Ended one/ })).toHaveTextContent(
      "ended",
    );
    await open(/Re-check filebrowser/);
    expect(screen.getByText("Failed · Explore · haiku")).toBeInTheDocument();
    expect(screen.getByText("Out of credit")).toBeInTheDocument();
    expect(screen.getByText("/srv/worktrees/a1")).toBeInTheDocument();
  });

  it("names a subagent that is called by its role once", async () => {
    draw([run({ label: "Explore", type: "Explore", model: null })]);
    expect(screen.getByRole("button", { name: /Explore/ })).toHaveTextContent(
      /^Explore1 min 18 s$/,
    );
  });

  it("stops a subagent that runs and can be stopped", async () => {
    const onStop = vi.fn();
    draw(
      [
        run({
          state: "running",
          status: "run",
          canStop: true,
          runningSince: Date.now() - 5000,
          ranMs: null,
          parts: [
            { kind: "prompt", id: "p1", number: null, text: "Go" },
            { kind: "messages", id: "m1", texts: ["Reading the audit."] },
          ],
        }),
      ],
      vi.fn(),
      onStop,
    );
    await open(/Re-check filebrowser/);
    // What it said last stands under its name while it works.
    expect(screen.getAllByText("Reading the audit.")).toHaveLength(2);
    // The tokens of one that still runs are not missing yet.
    expect(screen.queryByText(/tokens not reported/)).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Stop" }));
    expect(onStop).toHaveBeenCalledExactlyOnceWith("a1");
  });

  it("opens the subagents a subagent started", async () => {
    draw([
      run({
        agents: [
          run({
            id: "a2",
            label: "Grep the logs",
            startedBy: "Re-check filebrowser audit items",
          }),
        ],
      }),
    ]);
    // Both stand in the list.
    expect(
      screen.getByRole("button", { name: /Grep the logs/ }),
    ).toBeInTheDocument();
    await open(/Re-check filebrowser/);
    await open(/Grep the logs/);
    expect(
      screen.getByRole("heading", { name: "Grep the logs" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Started by Re-check filebrowser audit items"),
    ).toBeInTheDocument();
  });

  it("shows the activity map from the chat's agent", async () => {
    const onMain = vi.fn();
    draw([], onMain);
    await userEvent.click(screen.getByRole("button", { name: /Claude Code/ }));
    expect(onMain).toHaveBeenCalledOnce();
  });
});
