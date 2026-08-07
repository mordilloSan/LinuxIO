import { beforeEach, describe, expect, it, vi } from "vitest";

import type { JobSnapshot } from "@/api/generated/linuxio-types";

const mocks = vi.hoisted(() => ({
  openJobAttachStream: vi.fn(),
  openJobDataStream: vi.fn(),
  request: vi.fn(),
  streamWriteChunks: vi.fn(),
  upload: vi.fn(),
  waitForStreamResult: vi.fn(),
}));

vi.mock("@/api/generated/client", () => ({
  default: { filebrowser: { upload: mocks.upload } },
}));

vi.mock("@/api/linuxio", () => ({
  openJobAttachStream: mocks.openJobAttachStream,
  openJobDataStream: mocks.openJobDataStream,
}));

vi.mock("@/api/linuxio-core", () => ({
  LinuxIOError: class LinuxIOError extends Error {
    constructor(
      message: string,
      public code?: string | number,
    ) {
      super(message);
      this.name = "LinuxIOError";
    }
  },
  request: mocks.request,
}));

vi.mock("@/api/stream-helpers", () => ({
  streamWriteChunks: mocks.streamWriteChunks,
  waitForStreamResult: mocks.waitForStreamResult,
}));

const { uploadContent } = await import("@/api/uploads");

function snapshot(overrides: Partial<JobSnapshot> = {}): JobSnapshot {
  return {
    created_at: "2026-01-01T00:00:00.000Z",
    id: "job-1",
    state: "running",
    type: "filebrowser.upload",
    updated_at: "2026-01-01T00:00:00.000Z",
    ...overrides,
  };
}

describe("uploadContent", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("throws the job's structured error when the start snapshot is terminal", async () => {
    // The bridge fails a no-overwrite save onto an existing file before any
    // transfer state exists, so the start reply already carries the 409.
    // Callers (the compose editor's overwrite dialog) depend on that code.
    const job = snapshot({
      error: { code: 409, message: "destination already exists" },
      state: "failed",
    });
    mocks.upload.mockResolvedValue(job);
    const onJobStart = vi.fn();

    await expect(
      uploadContent("/srv/docker-compose.yml", new Uint8Array(4), {
        onJobStart,
      }),
    ).rejects.toMatchObject({
      code: 409,
      message: "destination already exists",
    });

    expect(onJobStart).toHaveBeenCalledWith(job);
    expect(mocks.openJobDataStream).not.toHaveBeenCalled();
    expect(mocks.streamWriteChunks).not.toHaveBeenCalled();
  });

  it("streams the content over the job data stream when the job is live", async () => {
    mocks.upload.mockResolvedValue(snapshot());
    const stream = { status: "open" };
    mocks.openJobDataStream.mockReturnValue(stream);
    mocks.waitForStreamResult.mockResolvedValue(undefined);
    mocks.streamWriteChunks.mockResolvedValue(undefined);

    await uploadContent("/srv/note.md", new Uint8Array(4), {
      overwrite: true,
    });

    expect(mocks.upload).toHaveBeenCalledWith({
      targetPath: "/srv/note.md",
      size: "4",
      overwrite: true,
    });
    expect(mocks.openJobDataStream).toHaveBeenCalledWith("job-1", 0);
    expect(mocks.streamWriteChunks).toHaveBeenCalledTimes(1);
  });
});
