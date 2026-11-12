import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { beforeEach, describe, expect, it } from "vitest";

import type { PaneView } from "@/api/acp";
import { fireEvent, render, screen, within } from "@/test/render";

import PanePanel from "./PanePanel";

const WIDTH_KEY = "linuxio.assistant.paneWidth";

const pane: PaneView = {
  conn: 1,
  sessionId: "s",
  id: "lot-map",
  title: "Work map",
  plugin: "lot-map",
  tree: {
    type: "Box",
    props: { flexDirection: "column" },
    children: [{ type: "Text", props: {}, children: ["Nothing is running"] }],
  },
};

beforeEach(() => {
  window.localStorage.clear();
});

// Holds what the dock holds: whether the user opened or closed the card.
function Harness({
  room = null,
  running = false,
}: {
  room?: number | null;
  running?: boolean;
}) {
  const [open, setOpen] = useState<boolean | null>(null);
  return (
    <PanePanel
      onOpenChange={setOpen}
      open={open}
      pane={pane}
      room={room}
      running={running}
    />
  );
}

function setup(room: number | null = null) {
  const { rerender } = render(<Harness room={room} />);
  return {
    setRoom: (next: number) => rerender(<Harness room={next} />),
    // Closed, the card is out of the accessibility tree.
    card: () => screen.queryByRole("complementary", { name: "Work map" }),
    tab: () => screen.getByRole("button", { name: "Work map" }),
    edge: () => screen.getByRole("separator", { name: "Resize Work map" }),
  };
}

describe("PanePanel", () => {
  it("draws the pane on a card of the default width", () => {
    const { card } = setup();
    expect(card()).toHaveStyle({ width: "340px" });
    expect(card()).toHaveTextContent("Nothing is running");
  });

  it("opens at the width the user left it", () => {
    window.localStorage.setItem(WIDTH_KEY, "480");
    expect(setup().card()).toHaveStyle({ width: "480px" });
  });

  it("ignores a stored width outside the allowed range", () => {
    window.localStorage.setItem(WIDTH_KEY, "9000");
    expect(setup().card()).toHaveStyle({ width: "340px" });
  });

  it("widens when the right edge is dragged right, and remembers it", () => {
    const { card, edge } = setup();
    fireEvent.pointerDown(edge(), { clientX: 900, pointerId: 1 });
    fireEvent.pointerMove(edge(), { clientX: 1000, pointerId: 1 });
    expect(card()).toHaveStyle({ width: "440px" });
    // Nothing is stored until the drag ends.
    expect(window.localStorage.getItem(WIDTH_KEY)).toBeNull();
    fireEvent.pointerUp(edge(), { clientX: 1000, pointerId: 1 });
    expect(window.localStorage.getItem(WIDTH_KEY)).toBe("440");
    // A move after the drag ended changes nothing.
    fireEvent.pointerMove(edge(), { clientX: 1200, pointerId: 1 });
    expect(card()).toHaveStyle({ width: "440px" });
  });

  it("keeps the width between its limits while dragging", () => {
    const { card, edge } = setup();
    fireEvent.pointerDown(edge(), { clientX: 900, pointerId: 1 });
    fireEvent.pointerMove(edge(), { clientX: -2000, pointerId: 1 });
    expect(card()).toHaveStyle({ width: "240px" });
    fireEvent.pointerMove(edge(), { clientX: 2000, pointerId: 1 });
    expect(card()).toHaveStyle({ width: "720px" });
  });

  it("resizes from the keyboard", async () => {
    const { card, edge } = setup();
    edge().focus();
    await userEvent.keyboard("{ArrowRight}{ArrowRight}");
    expect(card()).toHaveStyle({ width: "372px" });
    expect(edge()).toHaveAttribute("aria-valuenow", "372");
    await userEvent.keyboard("{ArrowLeft}");
    expect(card()).toHaveStyle({ width: "356px" });
    await userEvent.keyboard("{Home}");
    expect(card()).toHaveStyle({ width: "240px" });
    await userEvent.keyboard("{End}");
    expect(card()).toHaveStyle({ width: "720px" });
    expect(window.localStorage.getItem(WIDTH_KEY)).toBe("720");
  });

  it("goes back to the default width on a double click", async () => {
    window.localStorage.setItem(WIDTH_KEY, "600");
    const { card, edge } = setup();
    await userEvent.dblClick(edge());
    expect(card()).toHaveStyle({ width: "340px" });
    expect(window.localStorage.getItem(WIDTH_KEY)).toBe("340");
  });

  it("has one button in its header, which closes it to its tab", async () => {
    const { card, tab } = setup();
    const buttons = within(card()!).getAllByRole("button");
    expect(buttons).toHaveLength(1);
    expect(tab()).toHaveAttribute("aria-expanded", "true");

    await userEvent.click(buttons[0]);
    expect(buttons[0]).toHaveAccessibleName("Close Work map");
    expect(card()).toBeNull();
    expect(tab()).toHaveAttribute("aria-expanded", "false");
  });

  it("opens again from its tab, and the tab closes it too", async () => {
    const { card, tab } = setup();
    await userEvent.click(tab());
    expect(card()).toBeNull();
    await userEvent.click(tab());
    expect(card()).toHaveTextContent("Nothing is running");
  });

  it("starts closed where the margin beside the chat cannot hold it", async () => {
    // 860 for the reading column plus 396 on each side: the card, its
    // inset and the tabs.
    const { card, tab, setRoom } = setup(1652);
    expect(card()).toHaveTextContent("Nothing is running");
    setRoom(1651);
    expect(card()).toBeNull();
    // Opening it there is the user's call, and it stays open.
    await userEvent.click(tab());
    setRoom(1200);
    expect(card()).toHaveTextContent("Nothing is running");
  });

  it("never grows wider than the chat area", async () => {
    window.localStorage.setItem(WIDTH_KEY, "700");
    const { card, tab } = setup(500);
    await userEvent.click(tab());
    // The area minus the card's inset and the tabs.
    expect(card()).toHaveStyle({ width: "444px" });
  });

  it("pulses its tab's dot while the agent works", () => {
    const { rerender } = render(<Harness running />);
    const tab = screen.getByRole("button", { name: "Work map" });
    expect(tab).toHaveClass("assistant-dock-tab--running");
    rerender(<Harness />);
    expect(tab).not.toHaveClass("assistant-dock-tab--running");
  });
});
