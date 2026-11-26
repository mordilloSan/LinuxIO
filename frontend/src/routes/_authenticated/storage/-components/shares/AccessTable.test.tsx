import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { AccountGroup, AccountUser, SambaUser } from "@/api";
import { renderWithTanStackRouter, screen, within } from "@/test/render";

import AccessTable from "./AccessTable";
import type { AccessTable as AccessTableValue } from "./sharedFolders";

const users = [
  { username: "alice", uid: 1000, gecos: "Alice Example,,," },
  { username: "bob", uid: 1001, gecos: "" },
] as AccountUser[];
const groups = [
  { name: "family", gid: 2000, members: ["alice", "bob"] },
  { name: "kids", gid: 2001, members: [] },
] as AccountGroup[];

const table: AccessTableValue = {
  access: [
    { kind: "user", name: "alice", level: "rw" },
    { kind: "group", name: "family", level: "ro" },
  ],
  everyone: "none",
};

const renderTable = (
  props: Partial<Parameters<typeof AccessTable>[0]> = {},
) => {
  const onChange = vi.fn();
  renderWithTanStackRouter(
    <AccessTable
      currentUser="alice"
      groups={groups}
      onChange={onChange}
      users={users}
      value={table}
      {...props}
    />,
  );
  return onChange;
};

describe("AccessTable", () => {
  it("shows each row's level, with who the row is", async () => {
    renderTable();
    const alice = await screen.findByRole("radiogroup", {
      name: "Access for alice",
    });
    expect(
      within(alice).getByRole("radio", { name: "Read and write" }),
    ).toBeChecked();
    expect(screen.getByText("you · Alice Example")).toBeInTheDocument();
    const family = screen.getByRole("radiogroup", {
      name: "Access for the group family",
    });
    expect(
      within(family).getByRole("radio", { name: "Read only" }),
    ).toBeChecked();
    expect(screen.getByText("group · 2 people")).toBeInTheDocument();
    const everyone = screen.getByRole("radiogroup", {
      name: "Access for everyone else",
    });
    expect(
      within(everyone).getByRole("radio", { name: "No access" }),
    ).toBeChecked();
  });

  it("changes a row's level and everyone else's", async () => {
    const user = userEvent.setup();
    const onChange = renderTable();
    await user.click(
      within(
        await screen.findByRole("radiogroup", { name: "Access for alice" }),
      ).getByRole("radio", { name: "No access" }),
    );
    expect(onChange).toHaveBeenLastCalledWith({
      access: [
        { kind: "user", name: "alice", level: "none" },
        { kind: "group", name: "family", level: "ro" },
      ],
      everyone: "none",
    });
    await user.click(
      within(
        screen.getByRole("radiogroup", { name: "Access for everyone else" }),
      ).getByRole("radio", { name: "Read only" }),
    );
    expect(onChange).toHaveBeenLastCalledWith({ ...table, everyone: "ro" });
  });

  it("adds a user or group that has no row, read only, and removes a row", async () => {
    const user = userEvent.setup();
    const onChange = renderTable();
    await user.click(
      await screen.findByRole("combobox", { name: "Add a user or group" }),
    );
    // alice and family have rows already.
    expect(screen.queryByRole("option", { name: "alice" })).toBeNull();
    expect(screen.getByRole("option", { name: "bob" })).toBeInTheDocument();
    await user.click(screen.getByRole("option", { name: "kids (group)" }));
    expect(onChange).toHaveBeenLastCalledWith({
      access: [...table.access, { kind: "group", name: "kids", level: "ro" }],
      everyone: "none",
    });
    await user.click(screen.getByRole("button", { name: "Remove alice" }));
    expect(onChange).toHaveBeenLastCalledWith({
      access: [{ kind: "group", name: "family", level: "ro" }],
      everyone: "none",
    });
  });

  it("says who cannot sign in from Windows or a Mac yet", async () => {
    const sambaUsers: SambaUser[] = [{ username: "bob", passwordStale: true }];
    renderTable({
      sambaUsers,
      value: {
        access: [
          { kind: "user", name: "alice", level: "rw" },
          { kind: "user", name: "bob", level: "ro" },
        ],
        everyone: "none",
      },
    });
    expect(
      await screen.findByText(
        "alice cannot sign in from Windows or a Mac yet.",
        { exact: false },
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByText("bob's password was changed outside LinuxIO", {
        exact: false,
      }),
    ).toBeInTheDocument();
    expect(
      screen.getAllByRole("link", { name: "Set it in Accounts" })[0],
    ).toHaveAttribute("href", "/accounts");
  });

  it("says nothing about sign-in for a row with no access", async () => {
    renderTable({
      sambaUsers: [],
      value: {
        access: [{ kind: "user", name: "bob", level: "none" }],
        everyone: "ro",
      },
    });
    await screen.findByRole("radiogroup", { name: "Access for bob" });
    expect(screen.queryByText(/cannot sign in/)).toBeNull();
  });
});
