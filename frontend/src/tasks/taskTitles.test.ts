import { describe, expect, it } from "vitest";

import type { TaskHistoryEntry, TaskSnapshot } from "@/api";

import {
  fileTaskText,
  taskEndedAt,
  taskOutcome,
  taskRunTime,
  taskTitle,
} from "./taskTitles";

const snapshot = (overrides: Partial<TaskSnapshot> = {}): TaskSnapshot => ({
  id: "task-1",
  type: "filebrowser.compress",
  state: "completed",
  created_at: "2026-11-10T10:00:00Z",
  updated_at: "2026-11-10T10:00:05Z",
  ...overrides,
});

const entry = (
  state: TaskSnapshot["state"],
  reason?: string,
): TaskHistoryEntry => ({ task: snapshot({ state }), reason });

describe("taskTitle", () => {
  it("uses the label of a scheduled run", () => {
    expect(
      taskTitle(
        snapshot({
          type: "scheduled.script",
          metadata: { label: "Nightly backup" },
        }),
      ),
    ).toBe("Nightly backup");
  });

  it("uses the stored label of a job that names what it does", () => {
    expect(
      taskTitle(
        snapshot({
          type: "storage.btrfs_scrub_start",
          metadata: { label: "Scrubbing btrfs filesystem" },
        }),
      ),
    ).toBe("Scrubbing btrfs filesystem");
  });

  it("names what a file job did to its label, as the bell does", () => {
    const titled = (type: string, label: string) =>
      taskTitle(snapshot({ type, metadata: { label } }));

    expect(titled("filebrowser.delete_batch", "12 items")).toBe(
      "Deleting 12 items",
    );
    expect(titled("filebrowser.copy_batch", "12 items")).toBe(
      "Copying 12 items",
    );
    expect(titled("filebrowser.move_batch", "report.pdf")).toBe(
      "Moving report.pdf",
    );
    expect(titled("filebrowser.trash_batch", "report.pdf")).toBe(
      "Moving report.pdf to Trash",
    );
    expect(titled("filebrowser.trash_restore", "2 items")).toBe(
      "Restoring 2 items from Trash",
    );
    expect(titled("filebrowser.trash_purge", "2 items")).toBe(
      "Deleting 2 items from Trash",
    );
    expect(titled("filebrowser.chmod_batch", "3 items")).toBe(
      "Changing permissions: 3 items",
    );
    // An archive is named by its file name, an extraction without its extension.
    expect(titled("filebrowser.compress", "/home/alice/backup.zip")).toBe(
      "Compressing backup.zip",
    );
    expect(titled("filebrowser.extract", "/home/alice/backup.tar.gz")).toBe(
      "Extracting backup",
    );
  });

  it("falls back to the route of a job without metadata", () => {
    expect(taskTitle(snapshot({ type: "storage.btrfs_scrub_start" }))).toBe(
      "Storage: btrfs scrub start",
    );
    expect(taskTitle(snapshot({ type: "docker.update_container" }))).toBe(
      "Docker: update container",
    );
  });

  it("names a scheduled run without a label", () => {
    expect(taskTitle(snapshot({ type: "scheduled.docker_update" }))).toBe(
      "Scheduled run",
    );
  });
});

describe("fileTaskText", () => {
  it("pairs the action of a file job with its object", () => {
    expect(fileTaskText("filebrowser.delete_batch", "3 items")).toBe(
      "Deleting 3 items",
    );
    expect(fileTaskText("filebrowser.chmod_batch", "1 item", "chown")).toBe(
      "Changing ownership: 1 item",
    );
  });

  it("leaves the object of any other job alone", () => {
    expect(
      fileTaskText("storage.btrfs_scrub_start", "Scrubbing btrfs filesystem"),
    ).toBe("Scrubbing btrfs filesystem");
  });
});

describe("taskOutcome", () => {
  it("names each ended state", () => {
    expect(taskOutcome(entry("completed"))).toBe("Completed");
    expect(taskOutcome(entry("failed"))).toBe("Failed");
    expect(taskOutcome(entry("canceled"))).toBe("Cancelled");
  });

  it("adds the reason of an interrupted job", () => {
    expect(taskOutcome(entry("interrupted", "reboot"))).toBe(
      "Interrupted (reboot)",
    );
    expect(taskOutcome(entry("interrupted"))).toBe("Interrupted");
  });
});

describe("taskEndedAt", () => {
  it("returns the end time exactly as received, with nanoseconds", () => {
    expect(
      taskEndedAt(
        snapshot({
          started_at: "2026-11-10T10:00:01.123456789Z",
          finished_at: "2026-11-10T10:00:04.987654321Z",
        }),
      ),
    ).toBe("2026-11-10T10:00:04.987654321Z");
  });

  it("falls back to the start, then to the creation", () => {
    expect(taskEndedAt(snapshot({ started_at: "2026-11-10T10:00:01Z" }))).toBe(
      "2026-11-10T10:00:01Z",
    );
    expect(taskEndedAt(snapshot())).toBe("2026-11-10T10:00:00Z");
  });
});

describe("taskRunTime", () => {
  it("formats how long the run took", () => {
    expect(
      taskRunTime(
        snapshot({
          started_at: "2026-11-10T10:00:00Z",
          finished_at: "2026-11-10T10:01:05Z",
        }),
      ),
    ).toBe("1m 5s");
  });

  it("is null when the run has no start or no end", () => {
    expect(
      taskRunTime(snapshot({ finished_at: "2026-11-10T10:01:05Z" })),
    ).toBeNull();
    expect(
      taskRunTime(snapshot({ started_at: "2026-11-10T10:00:00Z" })),
    ).toBeNull();
  });
});
