import { screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { render } from "@/test/render";

import GroupsTab from "./GroupsTab";

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();

  return {
    ...actual,
    useSuspenseQuery: () => ({
      data: [
        { gid: 100, isSystem: false, members: ["alice"], name: "users" },
        { gid: 1002, isSystem: false, members: [], name: "builders" },
      ],
    }),
  };
});

const authState = vi.hoisted(() => ({ privileged: true }));

vi.mock("@/hooks/useAuth", () => ({
  default: () => ({
    privileged: authState.privileged,
    user: { name: "admin" },
  }),
}));

vi.mock("./CreateGroupDialog", () => ({ default: () => null }));
vi.mock("./EditGroupMembersDialog", () => ({ default: () => null }));
vi.mock("./DeleteGroupDialog", () => ({ default: () => null }));

describe("GroupsTab", () => {
  afterEach(() => {
    authState.privileged = true;
  });

  it.each(["table", "card"] as const)(
    "offers no group changes in the %s view without sudo",
    (viewMode) => {
      authState.privileged = false;

      render(<GroupsTab viewMode={viewMode} />);

      expect(screen.getAllByText("builders").length).toBeGreaterThan(0);
      expect(
        screen.queryByRole("button", { name: "Edit Members" }),
      ).not.toBeInTheDocument();
      expect(
        screen.queryByRole("button", { name: "Delete Group" }),
      ).not.toBeInTheDocument();
    },
  );

  it.each(["table", "card"] as const)(
    "offers group changes in the %s view with sudo",
    (viewMode) => {
      render(<GroupsTab viewMode={viewMode} />);

      expect(
        screen.getAllByRole("button", { name: "Edit Members" }),
      ).toHaveLength(2);
      expect(
        screen.getAllByRole("button", { name: "Delete Group" }),
      ).toHaveLength(2);
    },
  );
});
