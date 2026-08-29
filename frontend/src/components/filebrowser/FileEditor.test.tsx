import { createRef } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import FileEditor, {
  type FileEditorHandle,
  getEditorLanguageName,
} from "@/components/filebrowser/FileEditor";
import { act, fireEvent, render, screen } from "@/test/render";

const mocks = vi.hoisted(() => ({
  json: vi.fn(() => ({ extension: "json" })),
  shell: vi.fn(() => ({ extension: "shell" })),
  yaml: vi.fn(() => ({ extension: "yaml" })),
}));

vi.mock("@codemirror/lang-json", () => ({
  json: mocks.json,
}));

vi.mock("@codemirror/legacy-modes/mode/shell", () => ({
  shell: mocks.shell,
}));

vi.mock("@codemirror/lang-yaml", () => ({
  yaml: mocks.yaml,
}));

vi.mock("@uiw/react-codemirror", () => ({
  default: ({
    extensions,
    id,
    onChange,
    readOnly,
    value,
  }: {
    extensions: unknown[];
    id: string;
    onChange: (value: string) => void;
    readOnly: boolean;
    value: string;
  }) => (
    <textarea
      aria-label="Code editor"
      data-extension-count={extensions.length}
      id={id}
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

  it.each([
    ["compose.yaml", "yaml"],
    ["data.json", "json"],
    ["script.bash", "shell"],
    ["script.sh", "shell"],
    ["script.py", "python"],
    ["Dockerfile", "dockerfile"],
    ["nginx.service", "ini"],
    ["index.html", "html"],
    ["main.ts", "typescript"],
    ["schema.sql", "sql"],
    ["README", "text"],
    ["notes.unknown", "text"],
  ])("selects %s as %s", (fileName, language) => {
    expect(getEditorLanguageName(fileName)).toBe(language);
  });

  it("loads YAML language support for YAML files", async () => {
    render(
      <FileEditor
        fileName="compose.yaml"
        filePath="/srv/compose.yaml"
        initialContent="services: {}"
        onSave={vi.fn(async () => true)}
      />,
    );

    const editor = await screen.findByRole("textbox", { name: "Code editor" });
    expect(editor).toHaveAttribute("data-extension-count", "4");
    expect(mocks.yaml).toHaveBeenCalledWith();
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
        initialVersion="v1"
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
    expect(onSave).toHaveBeenCalledWith("changed again", "v1");
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
        initialVersion="v1"
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
        initialVersion="v1"
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
        initialVersion="v2"
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

    await act(async () => {
      await editorRef.current?.save();
    });
    expect(onSave).toHaveBeenCalledWith("local draft", "v1");
  });

  it("does not save a read-only editor from the keyboard shortcut", async () => {
    const editorRef = createRef<FileEditorHandle>();
    const onSave = vi.fn(async () => true);
    render(
      <FileEditor
        fileName="notes.txt"
        filePath="/srv/notes.txt"
        initialContent="original"
        onSave={onSave}
        readOnly
        ref={editorRef}
      />,
    );
    await screen.findByRole("textbox", { name: "Code editor" });

    fireEvent.keyDown(document, { ctrlKey: true, key: "s" });

    expect(onSave).not.toHaveBeenCalled();
    await act(async () => {
      expect(await editorRef.current?.save()).toBe(false);
    });
    expect(onSave).not.toHaveBeenCalled();
  });
});
