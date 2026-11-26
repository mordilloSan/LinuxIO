import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { renderWithTanStackRouter, screen } from "@/test/render";

import PoolsLayout from "./PoolsLayout";
import { poolFixture, singlePool } from "./poolTestFixtures";

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

beforeEach(() => {
  vi.clearAllMocks();
  mocks.btrfsList.mockResolvedValue([poolFixture(), singlePool]);
});

const renderLayout = (selectedUuid: string | null, onSelect = vi.fn()) => {
  renderWithTanStackRouter(
    <PoolsLayout onSelect={onSelect} selectedUuid={selectedUuid}>
      <p>pool panel</p>
    </PoolsLayout>,
  );
  return onSelect;
};

describe("PoolsLayout", () => {
  it("lists a card for each mounted pool and opens one", async () => {
    const user = userEvent.setup();
    const onSelect = renderLayout(null);

    await user.click(
      await screen.findByRole("button", { name: "Open tank details" }),
    );
    expect(onSelect).toHaveBeenCalledWith("uuid-tank");
    expect(
      screen.getByRole("button", { name: "Open Storage pool 2 details" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("pool panel")).not.toBeInTheDocument();
  });

  it("folds the list into the selected pool beside its panel", async () => {
    const user = userEvent.setup();
    const onSelect = renderLayout("uuid-tank");

    expect(await screen.findByText("pool panel")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Open Storage pool 2 details" }),
    ).not.toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: "Close pool details" }),
    );
    expect(onSelect).toHaveBeenCalledWith(null);
  });

  it("closes the pool with Escape", async () => {
    const user = userEvent.setup();
    const onSelect = renderLayout("uuid-tank");
    await screen.findByText("pool panel");

    await user.keyboard("{Escape}");

    expect(onSelect).toHaveBeenCalledWith(null);
  });

  it("says so when the pool is not mounted", async () => {
    const user = userEvent.setup();
    const onSelect = renderLayout("uuid-gone");

    expect(
      await screen.findByText("This pool is not mounted, or no longer exists."),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Back to pools" }));
    expect(onSelect).toHaveBeenCalledWith(null);
    expect(
      screen.getByRole("link", { name: "Mount it from the Disks page" }),
    ).toHaveAttribute("href", "/storage/disks");
  });

  it("reads the pools again every ten seconds", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      renderLayout("uuid-tank");
      await screen.findByText("pool panel");
      const before = mocks.btrfsList.mock.calls.length;
      await vi.advanceTimersByTimeAsync(10_000);
      expect(mocks.btrfsList.mock.calls.length).toBeGreaterThan(before);
    } finally {
      vi.useRealTimers();
    }
  });
});
