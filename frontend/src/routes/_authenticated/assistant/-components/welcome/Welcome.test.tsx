import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { Alert } from "@/api";
import { render } from "@/test/render";

import Welcome from "./Welcome";

const alert = (over: Partial<Alert>): Alert => ({
  firstOccurrence: "2026-11-12T06:00:00Z",
  id: "a1",
  lastOccurrence: "2026-11-12T06:00:00Z",
  occurrenceCount: 1,
  seen: false,
  severity: "error",
  source: "updates",
  title: "System update failed",
  ...over,
});

describe("Welcome", () => {
  it("introduces the agent when nothing needs a look", () => {
    render(<Welcome agentLabel="Claude Code" alerts={[]} onAsk={vi.fn()} />);
    expect(
      screen.getByRole("heading", { name: "What should we look at?" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Ask Claude Code anything about this host."),
    ).toBeInTheDocument();
  });

  it("says how many alerts are worth a look", () => {
    const { rerender } = render(
      <Welcome agentLabel="Claude Code" alerts={[alert({})]} onAsk={vi.fn()} />,
    );
    expect(
      screen.getByText("1 thing worth a look on this server"),
    ).toBeInTheDocument();

    rerender(
      <Welcome
        agentLabel="Claude Code"
        alerts={[alert({}), alert({ id: "a2", title: "Disk almost full" })]}
        onAsk={vi.fn()}
      />,
    );
    expect(
      screen.getByText("2 things worth a look on this server"),
    ).toBeInTheDocument();
  });

  it("asks the card's prompt when a card is clicked", async () => {
    const onAsk = vi.fn();
    render(<Welcome agentLabel="Claude Code" alerts={[]} onAsk={onAsk} />);
    await userEvent.click(
      screen.getByRole("button", { name: /Find failed services/ }),
    );
    expect(onAsk).toHaveBeenCalledTimes(1);
    expect(onAsk.mock.calls[0][0]).toMatch(/systemd units that have failed/);
  });
});
