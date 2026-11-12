import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { PaneView } from "@/api/acp";
import { act, fireEvent, render, screen } from "@/test/render";

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

// Lets a test say how wide the chat area the card floats in is.
let reportRoom: (width: number) => void = () => {};

beforeEach(() => {
  window.localStorage.clear();
  vi.stubGlobal(
    "ResizeObserver",
    class {
      constructor(callback: ResizeObserverCallback) {
        reportRoom = (width) =>
          act(() =>
            callback(
              [{ contentRect: { width } } as ResizeObserverEntry],
              this as unknown as ResizeObserver,
            ),
          );
      }
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
});

function setup() {
  const onHide = vi.fn();
  render(<PanePanel onHide={onHide} pane={pane} />);
  return {
    onHide,
    card: screen.getByRole("complementary", { name: "Work map" }),
    edge: () => screen.getByRole("separator", { name: "Resize Work map" }),
  };
}

describe("PanePanel", () => {
  it("draws the pane on a card of the default width", () => {
    const { card } = setup();
    expect(card).toHaveStyle({ width: "340px" });
    expect(card).toHaveTextContent("Nothing is running");
  });

  it("opens at the width the user left it", () => {
    window.localStorage.setItem(WIDTH_KEY, "480");
    expect(setup().card).toHaveStyle({ width: "480px" });
  });

  it("ignores a stored width outside the allowed range", () => {
    window.localStorage.setItem(WIDTH_KEY, "9000");
    expect(setup().card).toHaveStyle({ width: "340px" });
  });

  it("widens when the left edge is dragged left, and remembers it", () => {
    const { card, edge } = setup();
    fireEvent.pointerDown(edge(), { clientX: 900, pointerId: 1 });
    fireEvent.pointerMove(edge(), { clientX: 800, pointerId: 1 });
    expect(card).toHaveStyle({ width: "440px" });
    // Nothing is stored until the drag ends.
    expect(window.localStorage.getItem(WIDTH_KEY)).toBeNull();
    fireEvent.pointerUp(edge(), { clientX: 800, pointerId: 1 });
    expect(window.localStorage.getItem(WIDTH_KEY)).toBe("440");
    // A move after the drag ended changes nothing.
    fireEvent.pointerMove(edge(), { clientX: 600, pointerId: 1 });
    expect(card).toHaveStyle({ width: "440px" });
  });

  it("keeps the width between its limits while dragging", () => {
    const { card, edge } = setup();
    fireEvent.pointerDown(edge(), { clientX: 900, pointerId: 1 });
    fireEvent.pointerMove(edge(), { clientX: 2000, pointerId: 1 });
    expect(card).toHaveStyle({ width: "240px" });
    fireEvent.pointerMove(edge(), { clientX: -2000, pointerId: 1 });
    expect(card).toHaveStyle({ width: "720px" });
  });

  it("resizes from the keyboard", async () => {
    const { card, edge } = setup();
    edge().focus();
    await userEvent.keyboard("{ArrowLeft}{ArrowLeft}");
    expect(card).toHaveStyle({ width: "372px" });
    expect(edge()).toHaveAttribute("aria-valuenow", "372");
    await userEvent.keyboard("{ArrowRight}");
    expect(card).toHaveStyle({ width: "356px" });
    await userEvent.keyboard("{Home}");
    expect(card).toHaveStyle({ width: "240px" });
    await userEvent.keyboard("{End}");
    expect(card).toHaveStyle({ width: "720px" });
    expect(window.localStorage.getItem(WIDTH_KEY)).toBe("720");
  });

  it("goes back to the default width on a double click", async () => {
    window.localStorage.setItem(WIDTH_KEY, "600");
    const { card, edge } = setup();
    await userEvent.dblClick(edge());
    expect(card).toHaveStyle({ width: "340px" });
    expect(window.localStorage.getItem(WIDTH_KEY)).toBe("340");
  });

  it("minimizes to its header and opens again", async () => {
    const { card } = setup();
    await userEvent.click(
      screen.getByRole("button", { name: "Minimize Work map" }),
    );
    expect(card).not.toHaveTextContent("Nothing is running");
    expect(card).toHaveClass("assistant-pane-card--minimized");
    expect(screen.queryByRole("separator")).toBeNull();
    await userEvent.click(
      screen.getByRole("button", { name: "Open Work map" }),
    );
    expect(card).toHaveTextContent("Nothing is running");
  });

  it("starts minimized where the margin beside the chat cannot hold it", async () => {
    const { card } = setup();
    // 860 for the reading column plus 372 on each side.
    reportRoom(1604);
    expect(card).toHaveTextContent("Nothing is running");
    reportRoom(1603);
    expect(card).toHaveClass("assistant-pane-card--minimized");
    // Opening it there is the user's call, and it stays open.
    await userEvent.click(
      screen.getByRole("button", { name: "Open Work map" }),
    );
    reportRoom(1200);
    expect(card).toHaveTextContent("Nothing is running");
  });

  it("never grows wider than the chat area", () => {
    window.localStorage.setItem(WIDTH_KEY, "700");
    const { card } = setup();
    reportRoom(500);
    // Minimized here; opened, it takes the area minus its insets.
    fireEvent.click(screen.getByRole("button", { name: "Open Work map" }));
    expect(card).toHaveStyle({ width: "468px" });
  });

  it("hides on request", async () => {
    const { onHide } = setup();
    await userEvent.click(
      screen.getByRole("button", { name: "Hide Work map" }),
    );
    expect(onHide).toHaveBeenCalledTimes(1);
  });
});
