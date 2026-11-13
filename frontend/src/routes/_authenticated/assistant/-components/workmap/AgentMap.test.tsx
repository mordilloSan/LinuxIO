import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import AgentMap from "./AgentMap";
import type { AgentRun } from "./agents";

const run = (over: Partial<AgentRun> = {}): AgentRun => ({
  id: "a1",
  label: "Re-check filebrowser audit items",
  type: "Explore",
  model: "haiku",
  prompt: "Read the audit and check each item.",
  result: "Two items are **fixed**.",
  background: false,
  startedBy: null,
  said: ["Reading the audit.", "Waiting for the test run to finish."],
  status: "ok",
  startedAt: null,
  tookMs: 78_000,
  tokens: 73_100,
  calls: [
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
  ],
  agents: [],
  ...over,
});

const draw = (agents: AgentRun[], onMain = vi.fn()) =>
  render(
    <AgentMap
      agentLabel="Claude Code"
      agents={agents}
      model="Fable 5.2"
      onMain={onMain}
      running={false}
      usage={{ used: 275_800, size: 1_000_000 }}
    />,
  );

describe("AgentMap", () => {
  it("shows the chat's agent beside its subagents", () => {
    draw([run(), run({ id: "a2", label: "Write docs", status: "err" })]);
    expect(
      screen.getByText("2 agents · select one for details"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /Claude Code/ }),
    ).toHaveTextContent("fable 5.2 · 275.8k tokens in context");
    expect(
      screen.getByRole("button", { name: /Re-check filebrowser/ }),
    ).toHaveTextContent("Explore · 1 min 18 s · 73.1k tokens");
    expect(
      screen.getByRole("button", { name: /Write docs/ }),
    ).toHaveTextContent("failed");
  });

  it("says so when the chat has no subagents", () => {
    draw([]);
    expect(
      screen.getByText("No subagents in this chat yet."),
    ).toBeInTheDocument();
  });

  it("opens a subagent to what it did, and goes back to it", async () => {
    draw([run()]);
    const node = screen.getByRole("button", { name: /Re-check filebrowser/ });
    await userEvent.click(node);

    const back = screen.getByRole("button", { name: "All agents" });
    expect(back).toHaveFocus();
    expect(
      screen.getByRole("heading", { name: "Re-check filebrowser audit items" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Finished · Explore · haiku")).toBeInTheDocument();
    expect(screen.getByText("Started by the main agent")).toBeInTheDocument();
    expect(screen.getByText("1 min 18 s · 73.1k tokens")).toBeInTheDocument();
    // What it said last stands under its name; all of it is under Messages.
    expect(
      screen.getAllByText("Waiting for the test run to finish."),
    ).toHaveLength(2);
    expect(screen.getByText("Messages (2)")).toBeInTheDocument();
    expect(screen.getByText("Tool calls (2)")).toBeInTheDocument();
    const [read, bash] = screen.getAllByRole("listitem");
    expect(read).toHaveTextContent("Readfilebrowser.md(lines 421-465)");
    expect(read).toHaveAttribute(
      "title",
      "/docs/audit/filebrowser.md · 120 ms",
    );
    expect(bash).toHaveTextContent("BashRun the testsfailed");
    expect(screen.getByText("fixed").tagName).toBe("STRONG");
    expect(screen.getByText("Prompt")).toBeInTheDocument();

    await userEvent.click(back);
    expect(
      screen.getByRole("button", { name: /Re-check filebrowser/ }),
    ).toHaveFocus();
  });

  it("says what it does not know about a subagent of a loaded chat", async () => {
    draw([run({ tokens: null, tookMs: null, calls: [], said: [] })]);
    await userEvent.click(
      screen.getByRole("button", { name: /Re-check filebrowser/ }),
    );
    expect(screen.getByText("tokens not reported")).toBeInTheDocument();
    expect(screen.getByText("No calls seen.")).toBeInTheDocument();
    expect(screen.queryByText(/Messages/)).toBeNull();
  });

  it("opens the subagents a subagent started", async () => {
    draw([run({ agents: [run({ id: "a2", label: "Grep the logs" })] })]);
    // Both stand in the list.
    expect(
      screen.getByRole("button", { name: /Grep the logs/ }),
    ).toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", { name: /Re-check filebrowser/ }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: /Grep the logs/ }),
    );
    expect(
      screen.getByRole("heading", { name: "Grep the logs" }),
    ).toBeInTheDocument();
  });

  it("marks a subagent sent to the background", async () => {
    draw([
      run({
        background: true,
        startedBy: "Re-check filebrowser audit items",
        result: null,
      }),
    ]);
    expect(
      screen.getByRole("button", { name: /Re-check filebrowser/ }),
    ).toHaveTextContent("Explore · background");
    await userEvent.click(
      screen.getByRole("button", { name: /Re-check filebrowser/ }),
    );
    expect(
      screen.getByText("Finished · Explore · haiku · background"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Started by Re-check filebrowser audit items"),
    ).toBeInTheDocument();
    expect(screen.queryByText("Result")).toBeNull();
  });

  it("shows the activity map from the chat's agent", async () => {
    const onMain = vi.fn();
    draw([], onMain);
    await userEvent.click(screen.getByRole("button", { name: /Claude Code/ }));
    expect(onMain).toHaveBeenCalledOnce();
  });
});
