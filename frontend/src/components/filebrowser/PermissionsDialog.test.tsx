import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

const apiMocks = vi.hoisted(() => ({
  usersGroupsUseQuery: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      filebrowser: {
        ...actual.linuxio.filebrowser,
        users_groups: {
          useQuery: apiMocks.usersGroupsUseQuery,
        },
      },
    },
  };
});

const { default: PermissionsDialog } = await import("./PermissionsDialog");

describe("PermissionsDialog", () => {
  it("updates permission checkboxes after selecting and deselecting them", async () => {
    apiMocks.usersGroupsUseQuery.mockReturnValue({
      data: { groups: ["root"], users: ["root"] },
    });

    const { user } = render(
      <PermissionsDialog
        currentMode="0755"
        group="root"
        isDirectory
        onClose={vi.fn()}
        onConfirm={vi.fn()}
        open
        owner="root"
        pathLabel="/lib64/"
        selectionCount={1}
      />,
    );

    const groupWrite = screen.getByRole("checkbox", { name: "Group write" });
    const ownerRead = screen.getByRole("checkbox", { name: "Owner read" });

    expect(groupWrite).not.toBeChecked();
    await user.click(groupWrite);
    expect(groupWrite).toBeChecked();
    await user.click(groupWrite);
    expect(groupWrite).not.toBeChecked();

    expect(ownerRead).toBeChecked();
    await user.click(ownerRead);
    expect(ownerRead).not.toBeChecked();
  });
});
