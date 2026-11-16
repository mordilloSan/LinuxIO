import { beforeEach, describe, expect, it, vi } from "vitest";

import { fireEvent, render, screen } from "@/test/render";

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

  it("hands the settings to the page on Apply and shows no progress of its own", async () => {
    const onClose = vi.fn();
    const onConfirm = vi.fn();
    const { user } = render(
      <PermissionsDialog
        currentMode="0755"
        group="root"
        isDirectory
        onClose={onClose}
        onConfirm={onConfirm}
        open
        owner="root"
        pathLabel="/srv/data"
        selectionCount={1}
      />,
    );

    await user.click(
      screen.getByRole("checkbox", {
        name: "Apply recursively to all files and subdirectories",
      }),
    );
    // The page closes the dialog as the change starts; the task tray shows
    // its progress and can cancel it.
    await user.click(screen.getByRole("button", { name: "Apply" }));
    expect(onConfirm).toHaveBeenCalledWith("755", true, "root", "root");
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    expect(screen.getByRole("dialog")).not.toHaveAttribute("aria-busy");

    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onClose).toHaveBeenCalledTimes(1);
    await user.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalledTimes(2);
    const backdrop = document.querySelector(".app-dialog__backdrop");
    expect(backdrop).not.toBeNull();
    fireEvent.click(backdrop as Element);
    expect(onClose).toHaveBeenCalledTimes(3);
  });

  it("opens with the settings it is given, as Try again does", () => {
    render(
      <PermissionsDialog
        currentMode="700"
        group="staff"
        isDirectory
        onClose={vi.fn()}
        onConfirm={vi.fn()}
        open
        owner="alice"
        pathLabel="2 items"
        recursive
        selectionCount={2}
      />,
    );

    expect(screen.getByRole("checkbox", { name: "Owner write" })).toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: "Group read" }),
    ).not.toBeChecked();
    expect(
      screen.getByRole("checkbox", {
        name: "Apply recursively to all files and subdirectories",
      }),
    ).toBeChecked();
    expect(screen.getByRole("combobox", { name: "Owner" })).toHaveValue(
      "alice",
    );
    expect(screen.getByRole("combobox", { name: "Group" })).toHaveValue(
      "staff",
    );
  });
});
