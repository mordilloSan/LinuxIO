import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode, RefObject } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { FileEditorHandle } from "@/components/filebrowser/FileEditor";
import { useFileBrowserEditorActions } from "@/hooks/filebrowser/useFileBrowserEditorActions";
import type { EditorSlice } from "@/hooks/filebrowser/useFileEditor";
import { act, renderHook } from "@/test/render";

const toastMocks = vi.hoisted(() => ({
  error: vi.fn(),
  success: vi.fn(),
}));

const apiMocks = vi.hoisted(() => ({
  uploadContent: vi.fn(),
}));

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
    uploadContent: apiMocks.uploadContent,
  };
});

function editorRef(content = "file body"): RefObject<FileEditorHandle | null> {
  return {
    current: {
      getContent: () => content,
      isDirty: () => true,
      save: vi.fn(async () => true),
    },
  };
}

function editorSlice(overrides: Partial<EditorSlice> = {}): EditorSlice {
  return {
    actions: {
      close: vi.fn(),
      dismissClosePrompt: vi.fn(),
      openFile: vi.fn(),
      promptClose: vi.fn(),
      setDirty: vi.fn(),
      setSaving: vi.fn(),
    },
    closeEditorDialog: false,
    editingPath: "/srv/note.md",
    editorRef: editorRef(),
    isEditorDirty: false,
    isSavingFile: false,
    showQuickSave: true,
    ...overrides,
  };
}

function setup(overrides: Partial<EditorSlice> = {}, client?: QueryClient) {
  const queryClient =
    client ??
    new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

  const editor = editorSlice(overrides);

  function wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
  }

  const utils = renderHook(() => useFileBrowserEditorActions({ editor }), {
    wrapper,
  });
  return { ...utils, editor, queryClient };
}

describe("useFileBrowserEditorActions", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    apiMocks.uploadContent.mockResolvedValue(undefined);
  });

  describe("close flow", () => {
    it("prompts for confirmation when there are unsaved changes", () => {
      const { result, editor } = setup({ isEditorDirty: true });

      act(() => result.current.handleCloseEditor());

      expect(editor.actions.promptClose).toHaveBeenCalledTimes(1);
      expect(editor.actions.close).not.toHaveBeenCalled();
    });

    it("closes immediately when the editor is clean", () => {
      const { result, editor } = setup({ isEditorDirty: false });

      act(() => result.current.handleCloseEditor());

      expect(editor.actions.close).toHaveBeenCalledTimes(1);
      expect(editor.actions.promptClose).not.toHaveBeenCalled();
    });

    it("keeps editing by dismissing the confirm dialog", () => {
      const { result, editor } = setup();

      act(() => result.current.handleKeepEditing());

      expect(editor.actions.dismissClosePrompt).toHaveBeenCalledTimes(1);
    });

    it("discards changes and exits", () => {
      const { result, editor } = setup();

      act(() => result.current.handleDiscardAndExit());

      expect(editor.actions.close).toHaveBeenCalledTimes(1);
    });
  });

  describe("save flow", () => {
    it("streams the editor content and reports success", async () => {
      const client = new QueryClient({
        defaultOptions: { queries: { retry: false } },
      });
      const invalidateSpy = vi.spyOn(client, "invalidateQueries");
      const { result, editor } = setup(
        { editorRef: editorRef("hello") },
        client,
      );

      await act(() => result.current.handleSaveContent("hello"));

      expect(apiMocks.uploadContent).toHaveBeenCalledTimes(1);
      const [path, bytes, options] = apiMocks.uploadContent.mock.calls[0];
      expect(path).toBe("/srv/note.md");
      expect(bytes).toHaveLength(5);
      expect(options).toEqual({
        chunkSize: 1024 * 1024,
        onJobStart: expect.any(Function),
        overwrite: true,
      });
      expect(toastMocks.success).toHaveBeenCalledWith(
        "File saved successfully!",
        expect.anything(),
      );
      expect(editor.actions.setSaving).toHaveBeenNthCalledWith(1, true);
      expect(editor.actions.setSaving).toHaveBeenLastCalledWith(false);
      expect(invalidateSpy).toHaveBeenCalledTimes(1);
    });

    it("does nothing when there is no editor or path", async () => {
      const { result, editor } = setup({ editingPath: null });

      await act(() => result.current.handleSaveContent("hello"));

      expect(apiMocks.uploadContent).not.toHaveBeenCalled();
      expect(editor.actions.setSaving).not.toHaveBeenCalled();
    });

    it("surfaces a save error and clears the saving flag", async () => {
      apiMocks.uploadContent.mockRejectedValue(new Error("stream broke"));
      const { result, editor } = setup();

      await act(() => result.current.handleSaveContent("hello"));

      expect(toastMocks.error).toHaveBeenCalledWith(
        "stream broke",
        expect.anything(),
      );
      expect(editor.actions.setSaving).toHaveBeenLastCalledWith(false);
    });

    it("routes save requests through the editor handle", async () => {
      const ref = editorRef();
      const { result } = setup({ editorRef: ref });

      await act(async () => {
        await result.current.handleSaveFile();
      });

      expect(ref.current?.save).toHaveBeenCalledTimes(1);
    });
  });

  describe("save-and-exit flow", () => {
    it("exits after a successful save", async () => {
      const { result, editor } = setup();

      await act(async () => {
        await result.current.handleSaveAndExit();
      });

      expect(editor.actions.close).toHaveBeenCalledTimes(1);
    });

    it("stays open when the save fails", async () => {
      const ref = editorRef();
      vi.mocked(ref.current!.save).mockResolvedValue(false);
      const { result, editor } = setup({ editorRef: ref });

      await act(async () => {
        await result.current.handleSaveAndExit();
      });

      expect(editor.actions.close).not.toHaveBeenCalled();
    });
  });
});
