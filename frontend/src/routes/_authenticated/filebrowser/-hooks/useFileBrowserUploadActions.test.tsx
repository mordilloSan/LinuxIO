import type { ChangeEvent, RefObject } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { act, renderHook } from "@/test/render";

import type { DroppedEntry } from "../-fileUpload";
import { useFileBrowserUploadActions } from "./useFileBrowserUploadActions";
import { type UploadSlice, useFileUpload } from "./useFileUpload";

const toastMocks = vi.hoisted(() => ({
  error: vi.fn(),
  info: vi.fn(),
  success: vi.fn(),
  warning: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: {
    error: toastMocks.error,
    info: toastMocks.info,
    success: toastMocks.success,
    warning: toastMocks.warning,
  },
}));

type Params = Parameters<typeof useFileBrowserUploadActions>[0];

function inputRef(): RefObject<HTMLInputElement | null> {
  return { current: { click: vi.fn() } as unknown as HTMLInputElement };
}

function uploadSlice(overrides: Partial<UploadSlice> = {}): UploadSlice {
  return {
    actions: {
      clearEntries: vi.fn(),
      closeDialog: vi.fn(),
      closeDialogFor: vi.fn(),
      mergeEntries: vi.fn(),
      openDialog: vi.fn(),
      setOwnUploads: vi.fn(),
      setStatus: vi.fn(),
    },
    fileInputRef: inputRef(),
    folderInputRef: inputRef(),
    ownUploads: false,
    uploadDialogOpen: false,
    uploadEntries: [],
    uploadStatus: "idle",
    uploadSummary: { files: 0, folders: 0 },
    ...overrides,
  };
}

function setup(overrides: Partial<Params> = {}) {
  const params: Params = {
    invalidateListing: vi.fn(),
    normalizedPath: "/srv/data",
    onContextMenuClose: vi.fn(),
    resolveCollisions: vi.fn(async (items: unknown[]) => ({
      kept: items,
      keepBoth: [],
      overwrite: false,
    })) as unknown as Params["resolveCollisions"],
    startUpload: vi.fn().mockResolvedValue({
      failures: [],
      uploaded: 0,
    }),
    upload: uploadSlice(),
    ...overrides,
  };

  const utils = renderHook(() => useFileBrowserUploadActions(params));
  return { ...utils, params };
}

function changeEvent(files: File[]): ChangeEvent<HTMLInputElement> {
  return {
    target: { files: files as unknown as FileList, value: "anything" },
  } as ChangeEvent<HTMLInputElement>;
}

const entry: DroppedEntry = {
  file: new File(["x"], "a.txt"),
  isDirectory: false,
  relativePath: "a.txt",
};

describe("useFileBrowserUploadActions", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("opens the upload dialog with a clean entry list", () => {
    const { result, params } = setup();

    act(() => result.current.handleUpload());

    expect(params.onContextMenuClose).toHaveBeenCalledTimes(1);
    expect(params.upload.actions.openDialog).toHaveBeenCalledTimes(1);
  });

  it("clicks the hidden file and folder inputs when picking", () => {
    const { result, params } = setup();

    act(() => result.current.handlePickFiles());
    act(() => result.current.handlePickFolder());

    expect(params.upload.fileInputRef.current?.click).toHaveBeenCalledTimes(1);
    expect(params.upload.folderInputRef.current?.click).toHaveBeenCalledTimes(
      1,
    );
  });

  it("merges selected files into the upload entries and resets the input", () => {
    const { result, params } = setup();
    const event = changeEvent([new File(["data"], "a.txt")]);

    act(() => result.current.handleUploadInputChange(event));

    expect(params.upload.actions.mergeEntries).toHaveBeenCalledWith([
      expect.objectContaining({ isDirectory: false, relativePath: "a.txt" }),
    ]);
    expect(event.target.value).toBe("");
  });

  it("ignores an empty file selection without touching entries", () => {
    const { result, params } = setup();
    const event = changeEvent([]);

    act(() => result.current.handleUploadInputChange(event));

    expect(params.upload.actions.mergeEntries).not.toHaveBeenCalled();
    expect(toastMocks.error).not.toHaveBeenCalled();
    expect(event.target.value).toBe("");
  });

  it("warns when a selection yields no usable entries", () => {
    const { result, params } = setup();
    const event = changeEvent([new File([], "")]);

    act(() => result.current.handleUploadInputChange(event));

    expect(params.upload.actions.mergeEntries).not.toHaveBeenCalled();
    expect(toastMocks.error).toHaveBeenCalledWith(
      "No files detected in selection",
      expect.anything(),
    );
  });

  it("does not close or clear the dialog while it checks the destination", () => {
    const { result, params } = setup({
      upload: uploadSlice({ uploadStatus: "checking" }),
    });

    act(() => result.current.handleCloseUploadDialog());
    act(() => result.current.handleClearUploadSelection());

    expect(params.upload.actions.closeDialog).not.toHaveBeenCalled();
    expect(params.upload.actions.clearEntries).not.toHaveBeenCalled();
  });

  it("closes the dialog while the upload runs but keeps its entries", () => {
    const { result, params } = setup({
      upload: uploadSlice({ uploadStatus: "uploading" }),
    });

    act(() => result.current.handleClearUploadSelection());
    act(() => result.current.handleCloseUploadDialog());

    expect(params.upload.actions.clearEntries).not.toHaveBeenCalled();
    expect(params.upload.actions.closeDialog).toHaveBeenCalledTimes(1);
  });

  it("closes and clears the dialog when idle", () => {
    const { result, params } = setup({
      upload: uploadSlice({ uploadStatus: "idle" }),
    });

    act(() => result.current.handleCloseUploadDialog());

    expect(params.upload.actions.closeDialog).toHaveBeenCalledTimes(1);
  });

  it("rejects starting an upload with no entries", async () => {
    const { result, params } = setup({
      upload: uploadSlice({ uploadEntries: [] }),
    });

    await act(async () => {
      await result.current.handleStartUpload();
    });

    expect(toastMocks.error).toHaveBeenCalledWith(
      "Select files or folders to upload",
      expect.anything(),
    );
    expect(params.startUpload).not.toHaveBeenCalled();
  });

  it("uploads entries, refreshes the listing, and closes when there are no conflicts", async () => {
    const startUpload = vi.fn().mockResolvedValue({
      failures: [],
      uploaded: 2,
    });
    const uploadEntries = [entry];
    const { result, params } = setup({
      startUpload,
      upload: uploadSlice({ uploadEntries }),
    });

    await act(async () => {
      await result.current.handleStartUpload();
    });

    expect(params.resolveCollisions).toHaveBeenCalledWith(
      [entry],
      expect.any(Function),
      "/srv/data",
      expect.any(Function),
    );
    const describeIncoming = (
      params.resolveCollisions as ReturnType<typeof vi.fn>
    ).mock.calls[0][3] as (item: DroppedEntry) => unknown;
    expect(describeIncoming(entry)).toEqual({
      modified: new Date(entry.file!.lastModified).toISOString(),
      size: 1,
    });
    const getDestPath = (params.resolveCollisions as ReturnType<typeof vi.fn>)
      .mock.calls[0][1] as (item: DroppedEntry) => string;
    expect(getDestPath(entry)).toBe("/srv/data/a.txt");
    expect(startUpload).toHaveBeenCalledWith(
      [entry],
      "/srv/data",
      false,
      [],
      undefined,
    );
    expect(params.invalidateListing).toHaveBeenCalledTimes(1);
    // Closed by the request that started the upload, not whatever is open.
    expect(params.upload.actions.closeDialogFor).toHaveBeenCalledTimes(1);
    expect(
      vi.mocked(params.upload.actions.closeDialogFor).mock.calls[0][0],
    ).toBe(uploadEntries);
    expect(params.upload.actions.closeDialog).not.toHaveBeenCalled();
    expect(vi.mocked(params.upload.actions.setStatus).mock.calls).toEqual([
      [uploadEntries, "checking"],
      [uploadEntries, "uploading"],
      [uploadEntries, "idle"],
    ]);
  });

  it("holds the dialog through the conflict prompt and frees it once bytes move", async () => {
    let resolvePrompt!: (resolution: unknown) => void;
    const resolveCollisions = vi.fn(
      () =>
        new Promise((resolve) => {
          resolvePrompt = resolve;
        }),
    ) as unknown as Params["resolveCollisions"];
    const startUpload = vi.fn().mockResolvedValue({
      failures: [],
      uploaded: 1,
    });
    const { result, params } = setup({
      resolveCollisions,
      startUpload,
      upload: uploadSlice({ uploadEntries: [entry] }),
    });
    const setStatus = vi.mocked(params.upload.actions.setStatus);

    let started!: Promise<void> | undefined;
    act(() => {
      started = result.current.handleStartUpload();
    });
    expect(setStatus).toHaveBeenLastCalledWith([entry], "checking");
    expect(startUpload).not.toHaveBeenCalled();

    await act(async () => {
      resolvePrompt({ kept: [entry], keepBoth: [], overwrite: false });
      await started;
    });

    // The dialog can be closed from the moment the upload starts.
    const uploadingCall = setStatus.mock.calls.findIndex(
      ([, status]) => status === "uploading",
    );
    expect(setStatus.mock.invocationCallOrder[uploadingCall]).toBeLessThan(
      startUpload.mock.invocationCallOrder[0],
    );
  });

  it("leaves an upload dialog opened again while the upload ran open", async () => {
    let finishUpload!: (result: {
      failures: never[];
      uploaded: number;
    }) => void;
    const startUpload = vi.fn(
      () =>
        new Promise((resolve) => {
          finishUpload = resolve;
        }),
    );
    const { result } = renderHook(() => {
      const upload = useFileUpload();
      const actions = useFileBrowserUploadActions({
        invalidateListing: vi.fn(),
        normalizedPath: "/srv/data",
        onContextMenuClose: vi.fn(),
        resolveCollisions: vi.fn(async (items: unknown[]) => ({
          kept: items,
          keepBoth: [],
          overwrite: false,
        })) as unknown as Params["resolveCollisions"],
        startUpload: startUpload as unknown as Params["startUpload"],
        upload,
      });
      return { actions, upload };
    });

    act(() => result.current.actions.handleUpload());
    act(() => result.current.upload.actions.mergeEntries([entry]));
    let started!: Promise<void> | undefined;
    await act(async () => {
      started = result.current.actions.handleStartUpload();
    });
    expect(startUpload).toHaveBeenCalledTimes(1);
    expect(result.current.upload.uploadStatus).toBe("uploading");

    // Closed while the upload runs, then opened again with other files.
    act(() => result.current.actions.handleCloseUploadDialog());
    expect(result.current.upload.uploadDialogOpen).toBe(false);
    act(() => result.current.actions.handleUpload());
    const other: DroppedEntry = {
      file: new File(["y"], "b.txt"),
      isDirectory: false,
      relativePath: "b.txt",
    };
    act(() => result.current.upload.actions.mergeEntries([other]));

    await act(async () => {
      finishUpload({ failures: [], uploaded: 1 });
      await started;
    });

    expect(result.current.upload.uploadDialogOpen).toBe(true);
    expect(result.current.upload.uploadEntries).toEqual([other]);
    expect(result.current.upload.uploadStatus).toBe("idle");
  });

  it("keeps the dialog open when the conflict prompt is cancelled", async () => {
    const resolveCollisions = vi
      .fn()
      .mockResolvedValue(null) as unknown as Params["resolveCollisions"];
    const startUpload = vi.fn();
    const { result, params } = setup({
      resolveCollisions,
      startUpload,
      upload: uploadSlice({ uploadEntries: [entry] }),
    });

    await act(async () => {
      await result.current.handleStartUpload();
    });

    expect(startUpload).not.toHaveBeenCalled();
    expect(params.upload.actions.closeDialogFor).not.toHaveBeenCalled();
    expect(params.upload.actions.setStatus).toHaveBeenLastCalledWith(
      [entry],
      "idle",
    );
  });

  it("sends only kept items with overwrite when the user resolves conflicts", async () => {
    const other: DroppedEntry = {
      file: new File(["y"], "b.txt"),
      isDirectory: false,
      relativePath: "b.txt",
    };
    const resolveCollisions = vi.fn().mockResolvedValue({
      kept: [other, entry],
      keepBoth: [entry],
      overwrite: true,
    }) as unknown as Params["resolveCollisions"];
    const startUpload = vi.fn().mockResolvedValue({
      failures: [],
      uploaded: 1,
    });
    const { result } = setup({
      resolveCollisions,
      startUpload,
      upload: uploadSlice({ uploadEntries: [entry, other] }),
    });

    await act(async () => {
      await result.current.handleStartUpload();
    });

    expect(startUpload).toHaveBeenCalledWith(
      [entry, other],
      "/srv/data",
      true,
      ["a.txt"],
      undefined,
    );
  });

  it("asks for the session owner when the user ticked the ownership box", async () => {
    const startUpload = vi.fn().mockResolvedValue({
      failures: [],
      uploaded: 1,
    });
    const { result } = setup({
      startUpload,
      upload: uploadSlice({ ownUploads: true, uploadEntries: [entry] }),
    });

    await act(async () => {
      await result.current.handleStartUpload();
    });

    expect(startUpload).toHaveBeenCalledWith(
      [entry],
      "/srv/data",
      false,
      [],
      "session",
    );
  });

  it("does not start a task when every item is skipped", async () => {
    const resolveCollisions = vi.fn().mockResolvedValue({
      kept: [],
      keepBoth: [],
      overwrite: false,
    }) as unknown as Params["resolveCollisions"];
    const startUpload = vi.fn();
    const { result, params } = setup({
      resolveCollisions,
      startUpload,
      upload: uploadSlice({ uploadEntries: [entry] }),
    });

    await act(async () => {
      await result.current.handleStartUpload();
    });

    expect(startUpload).not.toHaveBeenCalled();
    expect(toastMocks.info).toHaveBeenCalledWith(
      "All items skipped",
      expect.anything(),
    );
    expect(params.upload.actions.setStatus).toHaveBeenLastCalledWith(
      [entry],
      "idle",
    );
  });

  it("reports a failed upload and always frees the dialog", async () => {
    const startUpload = vi.fn().mockRejectedValue(new Error("network down"));
    const { result, params } = setup({
      startUpload,
      upload: uploadSlice({ uploadEntries: [entry] }),
    });

    await act(async () => {
      await result.current.handleStartUpload();
    });

    expect(toastMocks.error).toHaveBeenCalledWith(
      "Upload failed",
      expect.anything(),
    );
    expect(params.upload.actions.closeDialogFor).not.toHaveBeenCalled();
    expect(params.upload.actions.setStatus).toHaveBeenLastCalledWith(
      [entry],
      "idle",
    );
  });
});
