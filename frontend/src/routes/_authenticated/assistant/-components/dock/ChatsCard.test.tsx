import type * as acp from "@agentclientprotocol/sdk/unstable";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { render, screen, within } from "@/test/render";

import { groupChats } from "./chatGroups";
import ChatsCard from "./ChatsCard";

// Local noon, so "today" and "yesterday" hold in any time zone.
const NOW = new Date(2026, 10, 12, 12, 0, 0).getTime();
const at = (daysAgo: number, hour = 9) =>
  new Date(2026, 10, 12 - daysAgo, hour, 0, 0).toISOString();

const chat = (
  sessionId: string,
  title: string | null,
  updatedAt?: string,
): acp.SessionInfo => ({ sessionId, cwd: "/", title, updatedAt });

const sessions = [
  chat("s1", "Full health check", at(0, 11)),
  chat("s2", "Reboot gap on 8 Nov", at(0, 8)),
  chat("s3", "Auth errors after login", at(1)),
  chat("s4", null, at(9)),
  chat("s5", "No date"),
];

describe("groupChats", () => {
  it("groups chats by day, newest group first, and drops empty groups", () => {
    expect(
      groupChats(sessions, "", NOW).map((group) => [
        group.label,
        group.sessions.map((session) => session.sessionId),
      ]),
    ).toEqual([
      ["Today", ["s1", "s2"]],
      ["Yesterday", ["s3"]],
      ["Earlier", ["s4", "s5"]],
    ]);
  });

  it("keeps the chats whose title holds the search, whatever the case", () => {
    expect(
      groupChats(sessions, "  ERRORS ", NOW).flatMap((group) =>
        group.sessions.map((session) => session.sessionId),
      ),
    ).toEqual(["s3"]);
  });

  it("finds an untitled chat by its id", () => {
    expect(groupChats(sessions, "s4", NOW)).toHaveLength(1);
  });
});

function setup(over: Partial<Parameters<typeof ChatsCard>[0]> = {}) {
  const props = {
    busy: false,
    canDelete: true,
    canLoad: true,
    currentId: "s2",
    now: NOW,
    onClose: vi.fn(),
    onDelete: vi.fn(),
    onSelect: vi.fn(),
    open: true,
    sessions,
    ...over,
  };
  render(<ChatsCard {...props} />);
  return props;
}

describe("ChatsCard", () => {
  it("lists the chats under their day, the open one marked", () => {
    setup();
    const card = screen.getByRole("complementary", { name: "Chats" });
    expect(
      within(card)
        .getAllByRole("heading")
        .map((heading) => heading.textContent),
    ).toEqual(["Today", "Yesterday", "Earlier"]);
    expect(
      within(card).getByRole("button", { name: /^Reboot gap on 8 Nov/ }),
    ).toHaveAttribute("aria-current", "true");
    expect(
      within(card).getByRole("button", { name: /^Full health check/ }),
    ).not.toHaveAttribute("aria-current");
    // An untitled chat shows its session id.
    expect(within(card).getByRole("button", { name: /^s4/ })).toBeVisible();
  });

  it("opens the chat that is clicked", async () => {
    const { onSelect } = setup();
    await userEvent.click(
      screen.getByRole("button", { name: /^Auth errors after login/ }),
    );
    expect(onSelect).toHaveBeenCalledWith("s3");
  });

  it("narrows the list while the user types", async () => {
    setup();
    await userEvent.type(
      screen.getByRole("textbox", { name: "Search chats" }),
      "reboot",
    );
    expect(
      screen.queryByRole("button", { name: /^Full health check/ }),
    ).toBeNull();
    expect(
      screen.getByRole("button", { name: /^Reboot gap on 8 Nov/ }),
    ).toBeVisible();
  });

  it("says so when no chat matches the search", async () => {
    setup();
    await userEvent.type(
      screen.getByRole("textbox", { name: "Search chats" }),
      "zzz",
    );
    expect(screen.getByText("No chats match.")).toBeVisible();
  });

  it("says so when there are no chats yet", () => {
    setup({ sessions: [] });
    expect(screen.getByText("No previous chats")).toBeVisible();
  });

  it("asks to delete a chat by its label", async () => {
    const { onDelete } = setup();
    await userEvent.click(
      screen.getByRole("button", { name: "Delete Full health check" }),
    );
    expect(onDelete).toHaveBeenCalledWith({
      sessionId: "s1",
      label: "Full health check",
    });
  });

  it("offers no delete where the agent cannot delete chats", () => {
    setup({ canDelete: false });
    expect(screen.queryByRole("button", { name: /^Delete / })).toBeNull();
  });

  it("opens no chat during a turn, and keeps the running chat", () => {
    setup({ busy: true });
    expect(
      screen.getByRole("button", { name: /^Full health check/ }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Delete Reboot gap on 8 Nov" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Delete Full health check" }),
    ).toBeEnabled();
  });

  it("closes from its one header button", async () => {
    const { onClose } = setup();
    await userEvent.click(screen.getByRole("button", { name: "Close Chats" }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("leaves the accessibility tree while closed", () => {
    setup({ open: false });
    expect(screen.queryByRole("complementary", { name: "Chats" })).toBeNull();
  });
});
