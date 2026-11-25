import userEvent from "@testing-library/user-event";
import { act, createRef } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { PaneView } from "@/api/acp";
import {
  announce,
  call,
  chatOf,
  FINISHED,
  pair,
  stateOf,
} from "@/test/assistantSubagentFixture";
import { render, screen, within } from "@/test/render";

import AssistantDock, { type AssistantDockHandle } from "./AssistantDock";

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

const activity = {
  agentLabel: "Claude Code",
  model: null,
  usage: null,
  sessionId: "chat",
  subagents: [],
  blocks: [
    {
      kind: "user" as const,
      id: "u1",
      text: "Check the disks",
      attachments: [],
    },
  ],
};

beforeEach(() => {
  window.localStorage.clear();
});

const props = (over: Partial<Parameters<typeof AssistantDock>[0]> = {}) => ({
  activity: null,
  busy: false,
  canDelete: true,
  canList: true,
  canLoad: true,
  currentId: "s1",
  onDelete: vi.fn(),
  onSelect: vi.fn(),
  onStopAgent: vi.fn(),
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

  it("opens a card without closing the others", async () => {
    render(<AssistantDock {...props({ pane })} />);
    await userEvent.click(tab("Chats"));
    expect(card("Chats")).not.toBeNull();
    expect(card("Work map")).not.toBeNull();

    await userEvent.click(tab("Work map"));
    expect(card("Work map")).toBeNull();
    await userEvent.click(tab("Work map"));
    expect(card("Work map")).not.toBeNull();
    expect(card("Chats")).not.toBeNull();
  });

  it("keeps the cards in one fixed order, whichever was opened first", async () => {
    render(<AssistantDock {...props({ activity, pane })} />);
    const order = (name: string) => card(name)?.style.order;
    await userEvent.click(tab("Activity"));
    await userEvent.click(tab("0 agents"));
    await userEvent.click(tab("Chats"));
    expect(order("Chats")).toBe("0");
    expect(order("Agents")).toBe("1");
    expect(order("Activity")).toBe("2");
    expect(order("Work map")).toBe("3");
  });

  it("gives every card the same width, and widens them together", async () => {
    render(<AssistantDock {...props({ activity })} />);
    await userEvent.click(tab("Chats"));
    await userEvent.click(tab("Activity"));
    await userEvent.click(tab("0 agents"));
    for (const name of ["Chats", "Agents", "Activity"]) {
      expect(card(name)).toHaveStyle({ width: "340px" });
    }

    screen.getByRole("separator", { name: "Resize Chats" }).focus();
    await userEvent.keyboard("{ArrowLeft}");
    for (const name of ["Chats", "Agents", "Activity"]) {
      expect(card(name)).toHaveStyle({ width: "356px" });
    }
  });

  it("opens a pane that arrives beside the chats that are open", async () => {
    const { rerender } = render(<AssistantDock {...props()} />);
    await userEvent.click(tab("Chats"));
    rerender(<AssistantDock {...props({ pane })} />);
    expect(card("Chats")).not.toBeNull();
    expect(card("Work map")).not.toBeNull();
  });

  it("keeps the activity map behind its tab until it is clicked", async () => {
    render(<AssistantDock {...props({ activity })} />);
    expect(card("Activity")).toBeNull();

    await userEvent.click(tab("Activity"));
    // The picture is drawn; the words under it open on request.
    expect(within(card("Activity")!).getByRole("img")).toBeInTheDocument();
    expect(card("Activity")).not.toHaveTextContent("new message");
    await userEvent.click(screen.getByRole("button", { name: /Details/ }));
    expect(card("Activity")).toHaveTextContent("new message");
    expect(tab("Activity")).toHaveAttribute("aria-expanded", "true");
  });

  it("has no activity tab without something to draw", () => {
    render(<AssistantDock {...props()} />);
    expect(screen.queryByRole("button", { name: "Activity" })).toBeNull();
  });

  it("opens the map and a plugin's pane together", async () => {
    render(<AssistantDock {...props({ activity, pane })} />);
    expect(card("Work map")).not.toBeNull();

    await userEvent.click(tab("Activity"));
    expect(card("Activity")).not.toBeNull();
    expect(card("Work map")).not.toBeNull();
  });

  it("keeps the map open while the chats open and close", async () => {
    render(<AssistantDock {...props({ activity })} />);
    await userEvent.click(tab("Activity"));
    await userEvent.click(tab("Chats"));
    expect(card("Activity")).not.toBeNull();
    expect(card("Chats")).not.toBeNull();

    await userEvent.click(tab("Chats"));
    expect(card("Activity")).not.toBeNull();
    expect(card("Chats")).toBeNull();
  });

  it("remembers that the map was open", async () => {
    const { unmount } = render(<AssistantDock {...props({ activity })} />);
    await userEvent.click(tab("Activity"));
    unmount();

    render(<AssistantDock {...props({ activity })} />);
    expect(card("Activity")).not.toBeNull();
  });

  it("counts the subagents at work on a tab that opens them", async () => {
    const { chat, subagents } = chatOf(
      [
        ["chat", announce("a", { title: "Check the disks" })],
        ...pair("p1", "chat", "a", "Check them"),
        ["a", call("c1", "Bash", { command: "lsblk" }, "pending")],
      ],
      [activity.blocks[0]],
    );
    const working = { ...activity, ...chat };
    const { rerender } = render(
      <AssistantDock
        {...props({ activity: { ...working, subagents }, running: true })}
      />,
    );
    expect(tab("1 agent")).toHaveAttribute("aria-expanded", "false");

    await userEvent.click(tab("1 agent"));
    expect(card("Agents")).toHaveTextContent("1 agent · 1 running");
    expect(tab("1 agent")).toHaveAttribute("aria-expanded", "true");
    // The map is its own card, closed until its tab is clicked.
    expect(tab("Activity")).toHaveAttribute("aria-expanded", "false");

    // Its state says when it is done, not the turn.
    const done = chatOf([
      ["chat", announce("a", { title: "Check the disks" })],
      ["chat", stateOf("a", FINISHED)],
    ]).subagents;
    rerender(
      <AssistantDock
        {...props({
          activity: { ...working, subagents: done },
          running: true,
        })}
      />,
    );
    expect(tab("0 agents")).toHaveAttribute("aria-expanded", "true");
  });

  it("opens the agents on one subagent when asked, and stops it from there", async () => {
    const { chat, subagents } = chatOf(
      [
        ["chat", announce("a", { title: "Check the disks" })],
        ...pair("p1", "chat", "a", "Check them"),
        ["a", call("c1", "Bash", { command: "lsblk" }, "pending")],
      ],
      [activity.blocks[0]],
    );
    const ref = createRef<AssistantDockHandle>();
    const onStopAgent = vi.fn();
    render(
      <AssistantDock
        {...props({
          activity: { ...activity, ...chat, subagents },
          onStopAgent,
          running: true,
        })}
        ref={ref}
      />,
    );
    expect(card("Agents")).toBeNull();

    // What a click on its name in the chat asks for.
    act(() => ref.current?.showAgent("a"));
    expect(card("Agents")).not.toBeNull();
    expect(
      screen.getByRole("heading", { name: "Check the disks" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Tool calls (1)")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Stop" }));
    expect(onStopAgent).toHaveBeenCalledExactlyOnceWith("a");
  });

  it("opens the map and the agents together, each on its own card", async () => {
    render(<AssistantDock {...props({ activity })} />);
    await userEvent.click(tab("Activity"));
    await userEvent.click(tab("0 agents"));
    expect(within(card("Activity")!).getByRole("img")).toBeInTheDocument();
    expect(card("Agents")).toHaveTextContent("No subagents in this chat yet.");

    // Each tab closes its own card and leaves the other.
    await userEvent.click(tab("Activity"));
    expect(card("Activity")).toBeNull();
    expect(card("Agents")).not.toBeNull();

    // The map's link to the agents opens theirs, and the agents' link to the
    // map opens the map, each leaving the card it came from.
    await userEvent.click(tab("Activity"));
    await userEvent.click(tab("Activity"));
    await userEvent.click(screen.getByRole("button", { name: /Claude Code/ }));
    expect(card("Activity")).not.toBeNull();
    expect(card("Agents")).not.toBeNull();

    // Closing the agents returns focus to their tab.
    await userEvent.click(screen.getByRole("button", { name: "Close Agents" }));
    expect(card("Agents")).toBeNull();
    expect(tab("0 agents")).toHaveFocus();
  });

  it("offers no chats where the agent cannot list them", () => {
    render(<AssistantDock {...props({ canList: false })} />);
    expect(tab("Chats")).toBeDisabled();
  });
});
