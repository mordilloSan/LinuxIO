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
  status: "ok",
  startedAt: null,
  tookMs: 78_000,
  tokens: 73_100,
  toolUses: 2,
  calls: [
    {
      id: "c1",
      station: "files",
      label: "audit/filebrowser.md",
      status: "ok",
      times: 1,
      tookMs: 120,
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
    expect(screen.getByText("haiku")).toBeInTheDocument();
    expect(screen.getByText("73.1k")).toBeInTheDocument();
    expect(screen.getByRole("listitem")).toHaveTextContent(
      "audit/filebrowser.md",
    );
    expect(screen.getByText("fixed").tagName).toBe("STRONG");
    expect(screen.getByText("Prompt")).toBeInTheDocument();

    await userEvent.click(back);
    expect(
      screen.getByRole("button", { name: /Re-check filebrowser/ }),
    ).toHaveFocus();
  });

  it("says what it does not know about a subagent of a loaded chat", async () => {
    draw([run({ tokens: null, tookMs: null, calls: [] })]);
    await userEvent.click(
      screen.getByRole("button", { name: /Re-check filebrowser/ }),
    );
    expect(screen.getByText("Not reported")).toBeInTheDocument();
    expect(screen.getByText("No calls seen.")).toBeInTheDocument();
    expect(screen.queryByText("Time")).toBeNull();
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

  it("shows the activity map from the chat's agent", async () => {
    const onMain = vi.fn();
    draw([], onMain);
    await userEvent.click(screen.getByRole("button", { name: /Claude Code/ }));
    expect(onMain).toHaveBeenCalledOnce();
  });
});
