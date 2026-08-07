import { createRef } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import FileEditor, {
  type FileEditorHandle,
} from "@/components/filebrowser/FileEditor";
import { act, fireEvent, render, screen } from "@/test/render";

vi.mock("ace-builds/src-noconflict/theme-monokai", () => ({}));

vi.mock("react-ace", () => ({
  default: ({
    onChange,
    readOnly,
    value,
  }: {
    onChange: (value: string) => void;
    readOnly: boolean;
    value: string;
  }) => (
    <textarea
      aria-label="Code editor"
      onChange={(event) => onChange(event.target.value)}
      readOnly={readOnly}
      value={value}
    />
  ),
}));

describe("FileEditor", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("notifies only dirty transitions and resets after a successful save", async () => {
    const editorRef = createRef<FileEditorHandle>();
    const onDirtyChange = vi.fn();
    const onSave = vi.fn(async () => true);
    render(
      <FileEditor
        fileName="notes.txt"
        filePath="/srv/notes.txt"
        initialContent="original"
        onDirtyChange={onDirtyChange}
        onSave={onSave}
        ref={editorRef}
      />,
    );
    const editor = await screen.findByRole("textbox", { name: "Code editor" });

    fireEvent.change(editor, { target: { value: "changed" } });
    fireEvent.change(editor, { target: { value: "changed again" } });

    expect(onDirtyChange).toHaveBeenCalledTimes(1);
    expect(onDirtyChange).toHaveBeenLastCalledWith(true);
    expect(editorRef.current?.isDirty()).toBe(true);

    let saved = false;
    await act(async () => {
      saved = (await editorRef.current?.save()) ?? false;
    });

    expect(saved).toBe(true);
    expect(onSave).toHaveBeenCalledWith("changed again");
    expect(onDirtyChange).toHaveBeenLastCalledWith(false);
    expect(editorRef.current?.isDirty()).toBe(false);
  });

  it("keeps the editor dirty when the save workflow declines completion", async () => {
    const editorRef = createRef<FileEditorHandle>();
    const onDirtyChange = vi.fn();
    const onSave = vi.fn(async () => false);
    render(
      <FileEditor
        fileName="notes.txt"
        filePath="/srv/notes.txt"
        initialContent="original"
        onDirtyChange={onDirtyChange}
        onSave={onSave}
        ref={editorRef}
      />,
    );
    const editor = await screen.findByRole("textbox", { name: "Code editor" });
    fireEvent.change(editor, { target: { value: "changed" } });

    let saved = true;
    await act(async () => {
      saved = (await editorRef.current?.save()) ?? false;
    });

    expect(saved).toBe(false);
    expect(onDirtyChange).toHaveBeenCalledTimes(1);
    expect(onDirtyChange).toHaveBeenLastCalledWith(true);
    expect(editorRef.current?.isDirty()).toBe(true);
  });

  it("preserves a dirty draft across background source refreshes", async () => {
    const editorRef = createRef<FileEditorHandle>();
    const onDirtyChange = vi.fn();
    const onSave = vi.fn(async () => true);
    const { rerender } = render(
      <FileEditor
        fileName="notes.txt"
        filePath="/srv/notes.txt"
        initialContent="original"
        onDirtyChange={onDirtyChange}
        onSave={onSave}
        ref={editorRef}
      />,
    );
    const editor = await screen.findByRole("textbox", { name: "Code editor" });
    fireEvent.change(editor, { target: { value: "local draft" } });

    rerender(
      <FileEditor
        fileName="notes.txt"
        filePath="/srv/notes.txt"
        initialContent="server refresh"
        onDirtyChange={onDirtyChange}
        onSave={onSave}
        ref={editorRef}
      />,
    );

    expect(screen.getByRole("textbox", { name: "Code editor" })).toHaveValue(
      "local draft",
    );
    expect(editorRef.current?.isDirty()).toBe(true);
    expect(onDirtyChange).toHaveBeenCalledTimes(1);
  });

  it("does not save a read-only editor from the keyboard shortcut", async () => {
    const onSave = vi.fn(async () => true);
    render(
      <FileEditor
        fileName="notes.txt"
        filePath="/srv/notes.txt"
        initialContent="original"
        onSave={onSave}
        readOnly
      />,
    );
    await screen.findByRole("textbox", { name: "Code editor" });

    fireEvent.keyDown(document, { ctrlKey: true, key: "s" });

    expect(onSave).not.toHaveBeenCalled();
  });
});
