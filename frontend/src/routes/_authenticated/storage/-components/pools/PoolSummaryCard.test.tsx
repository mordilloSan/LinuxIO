import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import PoolSummaryCard from "./PoolSummaryCard";
import { poolFixture, singlePool } from "./poolTestFixtures";

describe("PoolSummaryCard", () => {
  it("leads with health, disks, how data is kept and space", () => {
    render(<PoolSummaryCard fs={poolFixture()} name="tank" />);

    expect(screen.getByText("tank")).toBeInTheDocument();
    expect(screen.getByText("Healthy")).toBeInTheDocument();
    expect(
      screen.getByText("2 disks · two copies on different disks"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("41 GB stored · about 59 GB free"),
    ).toBeInTheDocument();
    expect(screen.getByText("sda · 100 GB")).toBeInTheDocument();
    expect(screen.getByText("sdb · 100 GB")).toBeInTheDocument();
  });

  it("names one disk without a plural and says when space is unknown", () => {
    render(
      <PoolSummaryCard
        fs={{ ...singlePool, allocation: [] }}
        name="Storage pool 1"
      />,
    );

    expect(screen.getByText(/^1 disk/)).toBeInTheDocument();
    expect(screen.getByText("Space unknown")).toBeInTheDocument();
  });

  it("opens the pool's page", async () => {
    const user = userEvent.setup();
    const onOpen = vi.fn();
    render(<PoolSummaryCard fs={poolFixture()} name="tank" onOpen={onOpen} />);

    await user.click(screen.getByRole("button", { name: "Open tank details" }));

    expect(onOpen).toHaveBeenCalled();
  });
});
