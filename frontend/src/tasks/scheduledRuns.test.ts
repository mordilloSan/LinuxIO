import { readFileSync } from "node:fs";
import path from "node:path";

import { describe, expect, it } from "vitest";

import type { TaskSnapshot } from "@/api";
import {
  TASK_TYPE_DOCKER_UPDATE,
  TASK_TYPE_STORAGE_BTRFS_BALANCE,
} from "@/tasks/taskTypes";
import type { BackgroundTask } from "@/tasks/types";

import {
  applyScheduledRunEvent,
  isScheduledRun,
  scheduledRunLabel,
  scheduledRunLink,
  withoutScheduledRuns,
} from "./scheduledRuns";

function scheduledRun(
  type: string,
  overrides: Partial<TaskSnapshot> = {},
): TaskSnapshot {
  return {
    created_at: "2026-11-10T03:00:00Z",
    id: "12345678-90ab-cdef-1234-567890abcdef",
    state: "running",
    type,
    updated_at: "2026-11-10T03:00:01Z",
    ...overrides,
  };
}

describe("isScheduledRun", () => {
  it.each([
    "scheduled.script",
    "scheduled.btrfs_snapshot",
    "scheduled.btrfs_replicate",
    "scheduled.btrfs_resync",
    "scheduled.docker_update",
  ])("is true for %s", (type) => {
    expect(isScheduledRun(type)).toBe(true);
  });

  it("is false for a browser-started job", () => {
    expect(isScheduledRun(TASK_TYPE_DOCKER_UPDATE)).toBe(false);
  });
});

describe("scheduledRunLabel", () => {
  it("adds the progress message to the label", () => {
    expect(
      scheduledRunLabel(
        scheduledRun("scheduled.btrfs_replicate", {
          metadata: { label: "Replication" },
          progress: { message: "Replicating 2 of 5" },
        }),
      ),
    ).toBe("Replication: Replicating 2 of 5");
  });

  it("is the schedule's name while a script reports no progress", () => {
    expect(
      scheduledRunLabel(
        scheduledRun("scheduled.script", {
          metadata: { label: "Nightly backup", identity: ["sched-1"] },
        }),
      ),
    ).toBe("Nightly backup");
  });

  it("falls back when the run has no label", () => {
    expect(scheduledRunLabel(scheduledRun("scheduled.script"))).toBe(
      "Scheduled run",
    );
  });
});

// The `to` paths the generated route tree knows: the keys of FileRoutesByTo.
function generatedRoutePaths(): Set<string> {
  const source = readFileSync(
    path.resolve(process.cwd(), "src/routeTree.gen.ts"),
    "utf8",
  );
  const block = /export interface FileRoutesByTo \{([^}]*)\}/.exec(source)?.[1];
  return new Set(
    [...(block ?? "").matchAll(/^\s*'([^']+)':/gm)].map((match) => match[1]),
  );
}

describe("scheduledRunLink targets", () => {
  const routes = generatedRoutePaths();

  it("reads the route paths from the generated route tree", () => {
    expect(routes.size).toBeGreaterThan(10);
    expect(routes.has("/")).toBe(true);
  });

  it.each([
    "scheduled.script",
    "scheduled.btrfs_snapshot",
    "scheduled.btrfs_replicate",
    "scheduled.btrfs_resync",
    "scheduled.docker_update",
    "scheduled.system_update",
  ])("links %s to a route the app has", (type) => {
    for (const metadata of [{ identity: ["id-1"] }, {}]) {
      expect(routes).toContain(
        scheduledRunLink(scheduledRun(type, { metadata })).to,
      );
    }
  });
});

describe("scheduledRunLink", () => {
  it("opens the Updates page for a system update, which the bell and the history list like any scheduled run", () => {
    const run = scheduledRun("scheduled.system_update", {
      metadata: { label: "System updates" },
    });
    expect(isScheduledRun(run.type)).toBe(true);
    expect(scheduledRunLink(run)).toEqual({ to: "/updates" });
  });

  it("opens the schedule a script belongs to", () => {
    expect(
      scheduledRunLink(
        scheduledRun("scheduled.script", {
          metadata: { label: "Nightly backup", identity: ["sched-1"] },
        }),
      ),
    ).toEqual({ to: "/services/schedules", search: { schedule: "sched-1" } });
  });

  it("opens the pool a resync runs on", () => {
    expect(
      scheduledRunLink(
        scheduledRun("scheduled.btrfs_resync", {
          metadata: { label: "Pool resync", identity: ["pool-uuid"] },
        }),
      ),
    ).toEqual({ to: "/storage", search: { pool: "pool-uuid" } });
  });

  it("opens the snapshots view for a snapshot run", () => {
    expect(
      scheduledRunLink(
        scheduledRun("scheduled.btrfs_snapshot", {
          metadata: { label: "Snapshots" },
        }),
      ),
    ).toEqual({ to: "/storage/subvolumes", search: { view: "snapshots" } });
  });

  it("opens the replication view for a replication run", () => {
    expect(
      scheduledRunLink(
        scheduledRun("scheduled.btrfs_replicate", {
          metadata: { label: "Replication" },
        }),
      ),
    ).toEqual({ to: "/storage/subvolumes", search: { view: "replication" } });
  });

  it("opens the containers page for a Docker update", () => {
    expect(
      scheduledRunLink(
        scheduledRun("scheduled.docker_update", {
          metadata: { label: "Container updates" },
        }),
      ),
    ).toEqual({ to: "/docker/containers" });
  });

  // A scheduled type added later must not borrow another type's page.
  it("opens the home page for a type it does not know", () => {
    expect(scheduledRunLink(scheduledRun("scheduled.future"))).toEqual({
      to: "/",
    });
  });
});

describe("applyScheduledRunEvent", () => {
  const balance: BackgroundTask = {
    abortController: new AbortController(),
    id: "balance-1",
    label: "Balancing btrfs filesystem",
    progress: 10,
    taskId: "balance-1",
    taskType: TASK_TYPE_STORAGE_BTRFS_BALANCE,
    type: "task",
  };
  const script = (overrides: Partial<TaskSnapshot> = {}) =>
    scheduledRun("scheduled.script", {
      metadata: { identity: ["sched-1"], label: "Nightly backup" },
      ...overrides,
    });

  it("adds a row for a running run", () => {
    expect(applyScheduledRunEvent([balance], script())).toEqual([
      balance,
      expect.objectContaining({
        cancelRequested: false,
        id: "12345678-90ab-cdef-1234-567890abcdef",
        indeterminate: true,
        label: "Nightly backup",
        link: { search: { schedule: "sched-1" }, to: "/services/schedules" },
        progress: 0,
        taskId: "12345678-90ab-cdef-1234-567890abcdef",
        taskType: "scheduled.script",
        type: "task",
      }),
    ]);
  });

  it("updates the run's row instead of adding a second one", () => {
    const rows = applyScheduledRunEvent([], script());
    const next = applyScheduledRunEvent(
      rows,
      script({
        cancel_requested: true,
        progress: { message: "Copying", percentage: 20 },
      }),
    );

    expect(next).toEqual([
      expect.objectContaining({
        cancelRequested: true,
        indeterminate: false,
        label: "Nightly backup: Copying",
        progress: 20,
      }),
    ]);
  });

  // The bell lists a row that leaves at 100% as completed, and a run may
  // still fail after reporting it.
  it("holds a run reporting 100% at 99%", () => {
    expect(
      applyScheduledRunEvent(
        [],
        script({ progress: { message: "Finishing", percentage: 100 } }),
      ),
    ).toEqual([
      expect.objectContaining({ indeterminate: false, progress: 99 }),
    ]);
  });

  it.each(["completed", "failed", "canceled", "interrupted"] as const)(
    "removes the row once the run is %s",
    (state) => {
      const rows = applyScheduledRunEvent([balance], script());
      expect(applyScheduledRunEvent(rows, script({ state }))).toEqual([
        balance,
      ]);
    },
  );

  it("leaves the rows alone when a run it never saw ends", () => {
    const rows = [balance];
    expect(applyScheduledRunEvent(rows, script({ state: "failed" }))).toBe(
      rows,
    );
  });
});

describe("withoutScheduledRuns", () => {
  const balance: BackgroundTask = {
    abortController: new AbortController(),
    id: "balance-1",
    label: "Balancing btrfs filesystem",
    progress: 10,
    taskType: TASK_TYPE_STORAGE_BTRFS_BALANCE,
    type: "task",
  };

  it("drops every scheduled row and keeps the rest", () => {
    const rows = applyScheduledRunEvent(
      applyScheduledRunEvent(
        [balance],
        scheduledRun("scheduled.script", { id: "a" }),
      ),
      scheduledRun("scheduled.btrfs_resync", { id: "b" }),
    );
    expect(withoutScheduledRuns(rows)).toEqual([balance]);
  });

  it("returns the same rows when none is scheduled", () => {
    const rows = [balance];
    expect(withoutScheduledRuns(rows)).toBe(rows);
  });
});
