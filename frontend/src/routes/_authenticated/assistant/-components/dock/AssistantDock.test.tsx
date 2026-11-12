import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { PaneView } from "@/api/acp";
import { render, screen } from "@/test/render";

import AssistantDock from "./AssistantDock";

const pane: PaneView = {
  conn: 1,
  sessionId: "s",
  id: "lot-map",
  title: "Work map",
  plugin: "lot-map",
  tree: {
    type: "Box",
    props: {},
    children: [{ type: "Text", props: {}, children: ["Nothing is running"] }],
  },
};

const props = (over: Partial<Parameters<typeof AssistantDock>[0]> = {}) => ({
  busy: false,
  canDelete: true,
  canList: true,
  canLoad: true,
  currentId: "s1",
  onDelete: vi.fn(),
  onSelect: vi.fn(),
  pane: null,
  running: false,
  sessions: [{ sessionId: "s1", cwd: "/", title: "Full health check" }],
  ...over,
});

const card = (name: string) => screen.queryByRole("complementary", { name });
const tab = (name: string) => screen.getByRole("button", { name });

describe("AssistantDock", () => {
  it("keeps the chats behind their tab until it is clicked", async () => {
    render(<AssistantDock {...props()} />);
    expect(card("Chats")).toBeNull();
    expect(tab("Chats")).toHaveAttribute("aria-expanded", "false");

    await userEvent.click(tab("Chats"));
    expect(card("Chats")).toHaveTextContent("Full health check");
    expect(tab("Chats")).toHaveAttribute("aria-expanded", "true");

    await userEvent.click(tab("Chats"));
    expect(card("Chats")).toBeNull();
  });

  it("closes the chats from the card and returns focus to their tab", async () => {
    render(<AssistantDock {...props()} />);
    await userEvent.click(tab("Chats"));
    await userEvent.click(screen.getByRole("button", { name: "Close Chats" }));
    expect(card("Chats")).toBeNull();
    expect(tab("Chats")).toHaveFocus();
  });

  it("shows a pane's tab only while there is a pane", () => {
    const { rerender } = render(<AssistantDock {...props()} />);
    expect(screen.queryByRole("button", { name: "Work map" })).toBeNull();
    rerender(<AssistantDock {...props({ pane })} />);
    expect(tab("Work map")).toHaveAttribute("aria-expanded", "true");
    expect(card("Work map")).toHaveTextContent("Nothing is running");
  });

  it("shows one card at a time", async () => {
    render(<AssistantDock {...props({ pane })} />);
    await userEvent.click(tab("Chats"));
    expect(card("Chats")).not.toBeNull();
    expect(card("Work map")).toBeNull();

    await userEvent.click(tab("Work map"));
    expect(card("Work map")).not.toBeNull();
    expect(card("Chats")).toBeNull();
  });

  it("leaves a pane that arrives while the chats are open behind its tab", async () => {
    const { rerender } = render(<AssistantDock {...props()} />);
    await userEvent.click(tab("Chats"));
    rerender(<AssistantDock {...props({ pane })} />);
    expect(card("Chats")).not.toBeNull();
    expect(card("Work map")).toBeNull();
  });

  it("offers no chats where the agent cannot list them", () => {
    render(<AssistantDock {...props({ canList: false })} />);
    expect(tab("Chats")).toBeDisabled();
  });
});
