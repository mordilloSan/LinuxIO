import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { render } from "@/test/render";

import AccountsGroupsPage from "./AccountsGroupsPage";

// The real tab registers its create handler on mount, as this stand-in does.
vi.mock("./GroupsTab", async () => {
  const { useEffect } = await import("react");
  function GroupsTabStub({
    onMountCreateHandler,
  }: {
    onMountCreateHandler?: (handler: () => void) => void;
  }) {
    useEffect(() => {
      onMountCreateHandler?.(() => {});
    }, [onMountCreateHandler]);
    return null;
  }
  return { default: GroupsTabStub };
});

describe("AccountsGroupsPage", () => {
  it("offers Add Group only to a session with sudo", async () => {
    const view = render(<AccountsGroupsPage />, { auth: { privileged: true } });
    expect(
      await screen.findByRole("button", { name: "Add Group" }),
    ).toBeInTheDocument();
    view.unmount();

    render(<AccountsGroupsPage />, { auth: { privileged: false } });
    expect(
      screen.queryByRole("button", { name: "Add Group" }),
    ).not.toBeInTheDocument();
  });
});
