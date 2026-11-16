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

  it("shows the job's progress and can be closed while it runs", async () => {
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
    // The controls that would start a second change stay held.
    expect(screen.getByRole("button", { name: "Applying…" })).toBeDisabled();
    expect(screen.getByRole("checkbox", { name: "Owner read" })).toBeDisabled();
    expect(
      screen.getByRole("checkbox", {
        name: "Apply recursively to all files and subdirectories",
      }),
    ).toBeDisabled();

    // The job runs on and the task tray keeps it, so every way out works.
    expect(
      screen.queryByRole("button", { name: "Cancel" }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Close" }));
    expect(onClose).toHaveBeenCalledTimes(1);
    await user.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalledTimes(2);
    const backdrop = document.querySelector(".app-dialog__backdrop");
    expect(backdrop).not.toBeNull();
    fireEvent.click(backdrop as Element);
    expect(onClose).toHaveBeenCalledTimes(3);
    expect(onConfirm).not.toHaveBeenCalled();
  });
});
