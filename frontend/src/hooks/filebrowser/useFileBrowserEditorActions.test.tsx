import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode, RefObject } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { FileEditorHandle } from "@/components/filebrowser/FileEditor";
import { useFileBrowserEditorActions } from "@/hooks/filebrowser/useFileBrowserEditorActions";
import { act, renderHook } from "@/test/render";

const toastMocks = vi.hoisted(() => ({
  error: vi.fn(),
  success: vi.fn(),
}));

const apiMocks = vi.hoisted(() => ({
  isConnected: vi.fn(),
  openJobDataStream: vi.fn(),
  upload: vi.fn(),
}));

const runChunkedMock = vi.hoisted(() => vi.fn());

vi.mock("sonner", () => ({
  toast: {
    error: toastMocks.error,
    info: vi.fn(),
    success: toastMocks.success,
    warning: vi.fn(),
  },
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    isConnected: apiMocks.isConnected,
    linuxio: {
      ...actual.linuxio,
      filebrowser: {
        ...actual.linuxio.filebrowser,
        upload: apiMocks.upload,
      },
    },
    openJobDataStream: apiMocks.openJobDataStream,
  };
});

vi.mock("@/hooks/useConfig", () => ({
  useConfig: () => ({ config: { appSettings: { chunkSizeMB: 1 } } }),
}));

vi.mock("@/hooks/useStreamResult", () => ({
  useStreamResult: () => ({ runChunked: runChunkedMock }),
}));

type Params = Parameters<typeof useFileBrowserEditorActions>[0];

function editorRef(content = "file body"): RefObject<FileEditorHandle | null> {
  return {
    current: { getContent: () => content } as unknown as FileEditorHandle,
  };
}

function setup(overrides: Partial<Params> = {}, client?: QueryClient) {
  const queryClient =
    client ??
    new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

  const params: Params = {
    editingPath: "/srv/note.md",
    editorRef: editorRef(),
    isEditorDirty: false,
    setCloseEditorDialog: vi.fn(),
    setEditingPath: vi.fn(),
    setIsEditorDirty: vi.fn(),
    setIsSavingFile: vi.fn(),
    ...overrides,
  };

  function wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
  }

  const utils = renderHook(() => useFileBrowserEditorActions(params), {
    wrapper,
  });
  return { ...utils, params, queryClient };
}

describe("useFileBrowserEditorActions", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    apiMocks.isConnected.mockReturnValue(true);
    apiMocks.upload.mockResolvedValue({ id: "job-1" });
    runChunkedMock.mockResolvedValue(undefined);
  });

  describe("close flow", () => {
    it("prompts for confirmation when there are unsaved changes", () => {
      const { result, params } = setup({ isEditorDirty: true });

      act(() => result.current.handleCloseEditor());

      expect(params.setCloseEditorDialog).toHaveBeenCalledWith(true);
      expect(params.setEditingPath).not.toHaveBeenCalled();
    });

    it("closes immediately when the editor is clean", () => {
      const { result, params } = setup({ isEditorDirty: false });

      act(() => result.current.handleCloseEditor());

      expect(params.setEditingPath).toHaveBeenCalledWith(null);
      expect(params.setIsEditorDirty).toHaveBeenCalledWith(false);
    });

    it("keeps editing by dismissing the confirm dialog", () => {
      const { result, params } = setup();

      act(() => result.current.handleKeepEditing());

      expect(params.setCloseEditorDialog).toHaveBeenCalledWith(false);
    });

    it("discards changes and exits", () => {
      const { result, params } = setup();

      act(() => result.current.handleDiscardAndExit());

      expect(params.setEditingPath).toHaveBeenCalledWith(null);
      expect(params.setIsEditorDirty).toHaveBeenCalledWith(false);
      expect(params.setCloseEditorDialog).toHaveBeenCalledWith(false);
    });
  });

  describe("save flow", () => {
    it("streams the editor content and reports success", async () => {
      const client = new QueryClient({
        defaultOptions: { queries: { retry: false } },
      });
      const invalidateSpy = vi.spyOn(client, "invalidateQueries");
      const { result, params } = setup(
        { editorRef: editorRef("hello") },
        client,
      );

      await act(async () => {
        await result.current.handleSaveFile();
      });

      expect(apiMocks.upload).toHaveBeenCalledWith({
        size: "5",
        targetPath: "/srv/note.md",
      });
      expect(runChunkedMock).toHaveBeenCalledTimes(1);
      expect(runChunkedMock.mock.calls[0][0]).toMatchObject({
        chunkSize: 1024 * 1024,
      });
      expect(toastMocks.success).toHaveBeenCalledWith(
        "File saved successfully!",
        expect.anything(),
      );
      expect(params.setIsEditorDirty).toHaveBeenCalledWith(false);
      expect(params.setIsSavingFile).toHaveBeenNthCalledWith(1, true);
      expect(params.setIsSavingFile).toHaveBeenLastCalledWith(false);
      expect(invalidateSpy).toHaveBeenCalledTimes(1);
    });

    it("aborts the save when the stream is not connected", async () => {
      apiMocks.isConnected.mockReturnValue(false);
      const { result, params } = setup();

      await act(async () => {
        await result.current.handleSaveFile();
      });

      expect(toastMocks.error).toHaveBeenCalledWith(
        "Stream connection not ready",
        expect.anything(),
      );
      expect(apiMocks.upload).not.toHaveBeenCalled();
      expect(params.setIsSavingFile).not.toHaveBeenCalled();
    });

    it("does nothing when there is no editor or path", async () => {
      const { result, params } = setup({ editingPath: null });

      await act(async () => {
        await result.current.handleSaveFile();
      });

      expect(apiMocks.upload).not.toHaveBeenCalled();
      expect(params.setIsSavingFile).not.toHaveBeenCalled();
    });

    it("surfaces a save error and clears the saving flag", async () => {
      runChunkedMock.mockRejectedValue(new Error("stream broke"));
      const { result, params } = setup();

      await act(async () => {
        await result.current.handleSaveFile();
      });

      expect(toastMocks.error).toHaveBeenCalledWith(
        "stream broke",
        expect.anything(),
      );
      expect(params.setIsSavingFile).toHaveBeenLastCalledWith(false);
    });
  });

  describe("save-and-exit flow", () => {
    it("exits after a successful save", async () => {
      const { result, params } = setup();

      await act(async () => {
        await result.current.handleSaveAndExit();
      });

      expect(params.setEditingPath).toHaveBeenCalledWith(null);
      expect(params.setCloseEditorDialog).toHaveBeenCalledWith(false);
    });

    it("stays open when the save fails", async () => {
      apiMocks.isConnected.mockReturnValue(false);
      const { result, params } = setup();

      await act(async () => {
        await result.current.handleSaveAndExit();
      });

      expect(params.setEditingPath).not.toHaveBeenCalled();
    });
  });
});
