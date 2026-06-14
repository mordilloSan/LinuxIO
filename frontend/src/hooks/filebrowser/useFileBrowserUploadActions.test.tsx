import type { ChangeEvent, RefObject } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useFileBrowserUploadActions } from "@/hooks/filebrowser/useFileBrowserUploadActions";
import type { DroppedEntry } from "@/hooks/filebrowser/useFileDroppedEntries";
import { act, renderHook } from "@/test/render";

const toastMocks = vi.hoisted(() => ({
  error: vi.fn(),
  success: vi.fn(),
  warning: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: {
    error: toastMocks.error,
    info: vi.fn(),
    success: toastMocks.success,
    warning: toastMocks.warning,
  },
}));

type Params = Parameters<typeof useFileBrowserUploadActions>[0];

function inputRef(): RefObject<HTMLInputElement | null> {
  return { current: { click: vi.fn() } as unknown as HTMLInputElement };
}

function setup(overrides: Partial<Params> = {}) {
  const params: Params = {
    fileInputRef: inputRef(),
    folderInputRef: inputRef(),
    invalidateListing: vi.fn(),
    isUploadProcessing: false,
    normalizedPath: "/srv/data",
    onContextMenuClose: vi.fn(),
    setIsUploadProcessing: vi.fn(),
    setOverwriteTargets: vi.fn(),
    setUploadDialogOpen: vi.fn(),
    setUploadEntries: vi.fn(),
    startUpload: vi.fn().mockResolvedValue({
      conflicts: [],
      failures: [],
      uploaded: 0,
    }),
    uploadEntries: [],
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
    expect(params.setUploadEntries).toHaveBeenCalledWith([]);
    expect(params.setUploadDialogOpen).toHaveBeenCalledWith(true);
  });

  it("clicks the hidden file and folder inputs when picking", () => {
    const { result, params } = setup();

    act(() => result.current.handlePickFiles());
    act(() => result.current.handlePickFolder());

    expect(params.fileInputRef.current?.click).toHaveBeenCalledTimes(1);
    expect(params.folderInputRef.current?.click).toHaveBeenCalledTimes(1);
  });

  it("merges selected files into the upload entries and resets the input", () => {
    const { result, params } = setup();
    const event = changeEvent([new File(["data"], "a.txt")]);

    act(() => result.current.handleUploadInputChange(event));

    expect(params.setUploadEntries).toHaveBeenCalledTimes(1);
    const updater = (params.setUploadEntries as ReturnType<typeof vi.fn>).mock
      .calls[0][0] as (prev: DroppedEntry[]) => DroppedEntry[];
    expect(updater([])).toEqual([
      expect.objectContaining({ isDirectory: false, relativePath: "a.txt" }),
    ]);
    expect(event.target.value).toBe("");
  });

  it("ignores an empty file selection without touching entries", () => {
    const { result, params } = setup();
    const event = changeEvent([]);

    act(() => result.current.handleUploadInputChange(event));

    expect(params.setUploadEntries).not.toHaveBeenCalled();
    expect(toastMocks.error).not.toHaveBeenCalled();
    expect(event.target.value).toBe("");
  });

  it("warns when a selection yields no usable entries", () => {
    const { result, params } = setup();
    const event = changeEvent([new File([], "")]);

    act(() => result.current.handleUploadInputChange(event));

    expect(params.setUploadEntries).not.toHaveBeenCalled();
    expect(toastMocks.error).toHaveBeenCalledWith(
      "No files detected in selection",
      expect.anything(),
    );
  });

  it("does not close or clear the dialog while an upload is processing", () => {
    const { result, params } = setup({ isUploadProcessing: true });

    act(() => result.current.handleCloseUploadDialog());
    act(() => result.current.handleClearUploadSelection());

    expect(params.setUploadDialogOpen).not.toHaveBeenCalled();
    expect(params.setUploadEntries).not.toHaveBeenCalled();
  });

  it("closes and clears the dialog when idle", () => {
    const { result, params } = setup({ isUploadProcessing: false });

    act(() => result.current.handleCloseUploadDialog());

    expect(params.setUploadDialogOpen).toHaveBeenCalledWith(false);
    expect(params.setUploadEntries).toHaveBeenCalledWith([]);
  });

  it("rejects starting an upload with no entries", async () => {
    const { result, params } = setup({ uploadEntries: [] });

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
      conflicts: [],
      failures: [],
      uploaded: 2,
    });
    const { result, params } = setup({ startUpload, uploadEntries: [entry] });

    await act(async () => {
      await result.current.handleStartUpload();
    });

    expect(startUpload).toHaveBeenCalledWith([entry], "/srv/data");
    expect(params.invalidateListing).toHaveBeenCalledTimes(1);
    expect(params.setUploadDialogOpen).toHaveBeenCalledWith(false);
    expect(params.setUploadEntries).toHaveBeenCalledWith([]);
    expect(params.setOverwriteTargets).not.toHaveBeenCalled();
    expect(params.setIsUploadProcessing).toHaveBeenNthCalledWith(1, true);
    expect(params.setIsUploadProcessing).toHaveBeenLastCalledWith(false);
  });

  it("surfaces conflicts and keeps the dialog open", async () => {
    const conflicts: DroppedEntry[] = [entry];
    const startUpload = vi.fn().mockResolvedValue({
      conflicts,
      failures: [],
      uploaded: 0,
    });
    const { result, params } = setup({ startUpload, uploadEntries: [entry] });

    await act(async () => {
      await result.current.handleStartUpload();
    });

    expect(params.setOverwriteTargets).toHaveBeenCalledWith(conflicts);
    expect(toastMocks.warning).toHaveBeenCalledWith(
      "1 item is already present. Overwrite them?",
      expect.anything(),
    );
    expect(params.invalidateListing).not.toHaveBeenCalled();
    expect(params.setUploadDialogOpen).not.toHaveBeenCalled();
  });

  it("reports a failed upload and always clears the processing flag", async () => {
    const startUpload = vi.fn().mockRejectedValue(new Error("network down"));
    const { result, params } = setup({ startUpload, uploadEntries: [entry] });

    await act(async () => {
      await result.current.handleStartUpload();
    });

    expect(toastMocks.error).toHaveBeenCalledWith(
      "Upload failed",
      expect.anything(),
    );
    expect(params.setIsUploadProcessing).toHaveBeenLastCalledWith(false);
  });
});
