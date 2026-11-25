import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { BtrfsReplicationJob, BtrfsSnapshot } from "@/api";
import { renderWithTanStackRouter, screen, within } from "@/test/render";

import PoolOverview from "./PoolOverview";
import { GiB, poolFixture, singlePool } from "./poolTestFixtures";

const snapshot = (createdAt: string): BtrfsSnapshot => ({
  source: "@",
  path: `linuxio-snapshots/@/${createdAt}-timeline`,
  kind: "timeline",
  createdAt,
  readOnly: true,
  id: 300,
  uuid: "",
  receivedUuid: "",
});

const job = (
  overrides: Partial<BtrfsReplicationJob> = {},
): BtrfsReplicationJob => ({
  id: "job-1",
  uuid: "uuid-tank",
  path: "@",
  target: { kind: "ssh", path: "/backup/tank", host: "nas2" },
  keepOnTarget: 0,
  enabled: true,
  after: "timeline",
  ...overrides,
});

const renderOverview = (
  props: Partial<Parameters<typeof PoolOverview>[0]> = {},
) => {
  const onTab = vi.fn();
  renderWithTanStackRouter(
    <PoolOverview
      fs={poolFixture()}
      jobs={[]}
      maintenance={null}
      onTab={onTab}
      snapshots={[]}
      {...props}
    />,
  );
  return onTab;
};

const protection = () => screen.findByRole("region", { name: "Protection" });

describe("PoolOverview", () => {
  it("says what a disk failure does for the weakest copy the pool keeps", async () => {
    renderOverview();
    expect(
      within(await protection()).getByText("Survives one disk failing"),
    ).toBeInTheDocument();
  });

  it("warns that one copy is lost with its disk", async () => {
    renderOverview({ fs: singlePool });
    expect(
      within(await protection()).getByText("A disk failure loses this pool"),
    ).toBeInTheDocument();
  });

  it("counts the copies of the weaker of data and metadata", async () => {
    renderOverview({
      fs: poolFixture({
        allocation: [
          { kind: "data", profile: "raid1c3", totalBytes: GiB, usedBytes: GiB },
          {
            kind: "metadata",
            profile: "raid1c4",
            totalBytes: GiB,
            usedBytes: GiB,
          },
        ],
      }),
    });
    expect(
      within(await protection()).getByText("Survives two disks failing"),
    ).toBeInTheDocument();
  });

  it("gives the last check's result", async () => {
    renderOverview({
      fs: poolFixture({
        scrub: {
          status: "finished",
          finishedAt: "2026-11-20T03:00:00Z",
          errorCount: 2,
          uncorrectableErrors: 2,
          correctedErrors: 0,
          bytesScrubbed: GiB,
          percent: 100,
        },
      }),
    });
    expect(
      within(await protection()).getByText(/2 errors it could not repair/),
    ).toBeInTheDocument();
  });

  it("says when the pool was never checked", async () => {
    renderOverview();
    expect(
      within(await protection()).getByText("Never checked"),
    ).toBeInTheDocument();
  });

  it("names the newest snapshot, or none", async () => {
    const user = userEvent.setup();
    const onTab = renderOverview({
      snapshots: [snapshot(new Date(Date.now() - 2 * 3600_000).toISOString())],
    });
    expect(
      within(await protection()).getByText("Newest 2h ago"),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "See snapshots" }));
    expect(onTab).toHaveBeenCalledWith("snapshots");
  });

  it("says there are no snapshots", async () => {
    renderOverview();
    expect(
      within(await protection()).getByText("No snapshots"),
    ).toBeInTheDocument();
  });

  it("says nothing copies the pool elsewhere", async () => {
    renderOverview({
      jobs: [job({ uuid: "another-pool" }), job({ enabled: false })],
    });
    expect(
      within(await protection()).getByText("No copy anywhere else"),
    ).toBeInTheDocument();
  });

  it("names where the pool is copied to", async () => {
    renderOverview({ jobs: [job()] });
    expect(
      within(await protection()).getByText("Copied to nas2:/backup/tank"),
    ).toBeInTheDocument();
  });

  it("shows each disk and sends Add a disk to the Disks tab", async () => {
    const user = userEvent.setup();
    const onTab = renderOverview();
    const disks = await screen.findByRole("region", { name: "Disks" });
    expect(within(disks).getByText("/dev/sda")).toBeInTheDocument();
    expect(within(disks).getByText("/dev/sdb")).toBeInTheDocument();
    await user.click(within(disks).getByRole("button", { name: /Add a disk/ }));
    expect(onTab).toHaveBeenCalledWith("disks");
  });

  it("lists the maintenance periods and links to Settings", async () => {
    const user = userEvent.setup();
    const onTab = renderOverview({
      maintenance: {
        tasks: [
          {
            task: "scrub",
            period: "monthly",
            periods: [],
            mountpoints: ["/mnt/tank"],
          },
          { task: "balance", period: "weekly", periods: [], mountpoints: [] },
        ],
      },
    });
    const card = await screen.findByRole("region", { name: "Maintenance" });
    expect(within(card).getByText("Monthly")).toBeInTheDocument();
    // Balance runs on other mountpoints only, and trim and defrag nowhere.
    expect(within(card).getAllByText("Off")).toHaveLength(3);
    await user.click(
      within(card).getByRole("button", { name: "Change in Settings" }),
    );
    expect(onTab).toHaveBeenCalledWith("settings");
  });

  it("counts a disk already missing against the margin", async () => {
    const missing = { ...poolFixture().devices[1], missing: true, path: "" };
    renderOverview({
      fs: poolFixture({
        degraded: true,
        devices: [poolFixture().devices[0], missing],
      }),
    });
    expect(
      within(await protection()).getByText(
        "Already missing a disk: another failure loses this pool",
      ),
    ).toBeInTheDocument();
  });

  it("says how many more disks a degraded pool survives", async () => {
    const [a, b] = poolFixture().devices;
    renderOverview({
      fs: poolFixture({
        degraded: true,
        allocation: [
          { kind: "data", profile: "raid1c3", totalBytes: GiB, usedBytes: GiB },
        ],
        devices: [a, b, { ...b, id: 3, missing: true, path: "" }],
      }),
    });
    expect(
      within(await protection()).getByText(
        "Missing a disk: survives one more disk failing",
      ),
    ).toBeInTheDocument();
  });

  it("says copies and maintenance are unknown until they are read", async () => {
    renderOverview({ jobs: undefined, maintenance: undefined });
    expect(
      within(await protection()).getByText("Copies unknown"),
    ).toBeInTheDocument();
    const card = await screen.findByRole("region", { name: "Maintenance" });
    expect(within(card).getByText("Reading the schedule…")).toBeInTheDocument();
  });
});
