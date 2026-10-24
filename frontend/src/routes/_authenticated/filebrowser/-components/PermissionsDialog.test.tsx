import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

const apiMocks = vi.hoisted(() => ({
  usersGroupsQueryOptions: vi.fn(),
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
          queryOptions: apiMocks.usersGroupsQueryOptions,
        },
      },
    },
  };
});

const { default: PermissionsDialog } = await import("./PermissionsDialog");

describe("PermissionsDialog", () => {
  beforeEach(() => {
    apiMocks.usersGroupsQueryOptions.mockReturnValue({
      initialData: { groups: ["root"], users: ["root"] },
      queryFn: () => Promise.resolve({ groups: ["root"], users: ["root"] }),
      queryKey: ["test", "users-groups"],
    });
  });

  it("updates permission checkboxes after selecting and deselecting them", async () => {
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

  it("shows Task progress and keeps pending permissions mounted", async () => {
    const onClose = vi.fn();
    const onConfirm = vi.fn();
    const { user } = render(
      <PermissionsDialog
        currentMode="0755"
        group="root"
        isDirectory
        isPending
        onClose={onClose}
        onConfirm={onConfirm}
        open
        owner="root"
        pathLabel="/srv/data"
        progress={{ phase: "chmod" }}
        selectionCount={1}
      />,
    );

    expect(screen.getByRole("status")).toHaveTextContent("chmod");
    expect(screen.getByRole("dialog")).toHaveAttribute("aria-busy", "true");
    expect(screen.getByRole("button", { name: "Applying…" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    expect(screen.getByRole("checkbox", { name: "Owner read" })).toBeDisabled();
    await user.keyboard("{Escape}");
    expect(onClose).not.toHaveBeenCalled();
    expect(onConfirm).not.toHaveBeenCalled();
  });
});
