import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { BtrfsFilesystem } from "@/api";
import { renderWithTanStackRouter } from "@/test/render";

import { Route } from "../route";

const mocks = vi.hoisted(() => ({ btrfsList: vi.fn() }));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      storage: {
        ...actual.linuxio.storage,
        btrfs_list: {
          queryKey: ["test-btrfs-list"],
          queryFn: () => mocks.btrfsList(),
        },
      },
    },
  };
});

const StorageLayout = Route.options.component!;

const renderLayout = (privileged: boolean) =>
  renderWithTanStackRouter(<StorageLayout />, {
    auth: { privileged },
    capabilities: { btrfsAvailable: true },
  });

describe("storage tabs", () => {
  it("offers Subvolumes once a btrfs filesystem is mounted", async () => {
    mocks.btrfsList.mockResolvedValue([{ uuid: "pool" } as BtrfsFilesystem]);
    renderLayout(true);
    expect(
      await screen.findByRole("tab", { name: "Subvolumes" }),
    ).toBeInTheDocument();
  });

  it("hides Subvolumes without a mounted btrfs filesystem", async () => {
    mocks.btrfsList.mockResolvedValue([]);
    renderLayout(true);
    expect(
      await screen.findByRole("tab", { name: "Disks" }),
    ).toBeInTheDocument();
    await waitFor(() => expect(mocks.btrfsList).toHaveBeenCalled());
    expect(
      screen.queryByRole("tab", { name: "Subvolumes" }),
    ).not.toBeInTheDocument();
  });

  it("hides Subvolumes from unprivileged sessions without asking", async () => {
    mocks.btrfsList.mockClear();
    renderLayout(false);
    expect(
      await screen.findByRole("tab", { name: "Disks" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("tab", { name: "Subvolumes" }),
    ).not.toBeInTheDocument();
    expect(mocks.btrfsList).not.toHaveBeenCalled();
  });
});
