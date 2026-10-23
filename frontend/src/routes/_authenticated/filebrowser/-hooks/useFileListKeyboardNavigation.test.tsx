import { useRef, type RefObject } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { fireEvent, render, screen } from "@/test/render";

import { useFileListKeyboardNavigation } from "./useFileListKeyboardNavigation";
import type { FileItem } from "../-types";

const items: FileItem[] = [
  { name: "alpha.txt", path: "/alpha.txt", type: "file" },
  { name: "beta.txt", path: "/beta.txt", type: "file" },
  { name: "another.txt", path: "/another.txt", type: "file" },
];

const gridItems: FileItem[] = [
  { name: "photos", path: "/photos/", type: "directory" },
  { name: "phone.txt", path: "/phone.txt", type: "file" },
  { name: "a.txt", path: "/a.txt", type: "file" },
  { name: "b.txt", path: "/b.txt", type: "file" },
  { name: "c.txt", path: "/c.txt", type: "file" },
  { name: "d.txt", path: "/d.txt", type: "file" },
  { name: "e.txt", path: "/e.txt", type: "file" },
];

function Harness({
  anchorIndex = -1,
  columnCount = 1,
  focusedIndex = -1,
  global = false,
  listItems = items,
  onDelete = vi.fn(),
  onDeletePermanently = vi.fn(),
  onDownloadFile = vi.fn(),
  onFocusChange = vi.fn(),
  onNavigateUp = vi.fn(),
  onOpenDirectory = vi.fn(),
  onRename = vi.fn(),
  onSelectionChange = vi.fn(),
}: {
  anchorIndex?: number;
  columnCount?: number;
  focusedIndex?: number;
  global?: boolean;
  listItems?: FileItem[];
  onDelete?: () => void;
  onDeletePermanently?: () => void;
  onDownloadFile?: (item: FileItem) => void;
  onFocusChange?: (index: number) => void;
  onNavigateUp?: () => void;
  onOpenDirectory?: (path: string) => void;
  onRename?: () => void;
  onSelectionChange?: (paths: Set<string>) => void;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const anchorIndexRef = useRef(anchorIndex);
  const columnCountRef = useRef(columnCount);

  useFileListKeyboardNavigation({
    allItems: listItems,
    anchorIndexRef,
    columnCountRef,
    containerRef: containerRef as RefObject<HTMLDivElement>,
    focusedIndex,
    global,
    onDelete,
    onDeletePermanently,
    onDownloadFile,
    onFocusChange,
    onNavigateUp,
    onOpenDirectory,
    onRename,
    onSelectionChange,
    selectedPaths:
      focusedIndex >= 0 ? new Set([listItems[focusedIndex].path]) : new Set(),
  });

  return (
    <div ref={containerRef} data-testid="files">
      {listItems.map((item) => (
        <div data-file-card="true" key={item.path}>
          {item.name}
        </div>
      ))}
      <input aria-label="rename" />
    </div>
  );
}

describe("useFileListKeyboardNavigation", () => {
  beforeEach(() => {
    Element.prototype.scrollIntoView = vi.fn();
  });

  it("selects all items with Ctrl+A and clears selection with Escape", () => {
    const onSelectionChange = vi.fn();
    const onFocusChange = vi.fn();

    render(
      <Harness
        onFocusChange={onFocusChange}
        onSelectionChange={onSelectionChange}
      />,
    );
    const container = screen.getByTestId("files");

    fireEvent.keyDown(container, { key: "a", ctrlKey: true });
    fireEvent.keyDown(container, { key: "Escape" });

    expect(Array.from(onSelectionChange.mock.calls[0][0])).toEqual([
      "/alpha.txt",
      "/beta.txt",
      "/another.txt",
    ]);
    expect(onSelectionChange.mock.calls[1][0].size).toBe(0);
    expect(onFocusChange).toHaveBeenCalledWith(-1);
  });

  it("moves focus to the next matching item by typed letter and wraps", () => {
    const onSelectionChange = vi.fn();
    const onFocusChange = vi.fn();

    render(
      <Harness
        focusedIndex={0}
        onFocusChange={onFocusChange}
        onSelectionChange={onSelectionChange}
      />,
    );

    fireEvent.keyDown(screen.getByTestId("files"), { key: "a" });

    expect(onFocusChange).toHaveBeenCalledWith(2);
    expect(Array.from(onSelectionChange.mock.calls[0][0])).toEqual([
      "/another.txt",
    ]);
  });

  it("calls delete and rename shortcuts", () => {
    const onDelete = vi.fn();
    const onRename = vi.fn();

    render(<Harness onDelete={onDelete} onRename={onRename} />);
    const container = screen.getByTestId("files");

    fireEvent.keyDown(container, { key: "Delete" });
    fireEvent.keyDown(container, { key: "F2" });

    expect(onDelete).toHaveBeenCalledTimes(1);
    expect(onRename).toHaveBeenCalledTimes(1);
  });

  it("sends Shift+Delete to the permanent delete shortcut only", () => {
    const onDelete = vi.fn();
    const onDeletePermanently = vi.fn();

    render(
      <Harness onDelete={onDelete} onDeletePermanently={onDeletePermanently} />,
    );
    fireEvent.keyDown(screen.getByTestId("files"), {
      key: "Delete",
      shiftKey: true,
    });

    expect(onDeletePermanently).toHaveBeenCalledTimes(1);
    expect(onDelete).not.toHaveBeenCalled();
  });

  it("does not intercept keyboard shortcuts while typing in inputs", () => {
    const onSelectionChange = vi.fn();

    render(<Harness onSelectionChange={onSelectionChange} />);
    fireEvent.keyDown(screen.getByLabelText("rename"), {
      key: "a",
      ctrlKey: true,
    });

    expect(onSelectionChange).not.toHaveBeenCalled();
  });

  it("selects all with Ctrl+A when CapsLock uppercases the key", () => {
    const onSelectionChange = vi.fn();

    render(<Harness onSelectionChange={onSelectionChange} />);
    fireEvent.keyDown(screen.getByTestId("files"), {
      key: "A",
      ctrlKey: true,
    });

    expect(onSelectionChange.mock.calls[0][0].size).toBe(items.length);
  });

  it("stays inert while a dialog or fullscreen overlay is open", () => {
    const onDelete = vi.fn();
    const onSelectionChange = vi.fn();
    const overlay = document.createElement("div");
    overlay.className = "app-dialog-root";
    document.body.appendChild(overlay);

    try {
      render(
        <Harness onDelete={onDelete} onSelectionChange={onSelectionChange} />,
      );
      const container = screen.getByTestId("files");

      const escape = fireEvent.keyDown(container, { key: "Escape" });
      fireEvent.keyDown(container, { key: "Delete" });

      // Not calling preventDefault leaves Escape for the dialog's own handler.
      expect(escape).toBe(true);
      expect(onSelectionChange).not.toHaveBeenCalled();
      expect(onDelete).not.toHaveBeenCalled();
    } finally {
      overlay.remove();
    }
  });

  it("can listen globally on document keydown", () => {
    const onFocusChange = vi.fn();
    const onSelectionChange = vi.fn();

    render(
      <Harness
        global
        onFocusChange={onFocusChange}
        onSelectionChange={onSelectionChange}
      />,
    );

    fireEvent.keyDown(document, { key: "b" });

    expect(onFocusChange).toHaveBeenCalledWith(1);
    expect(Array.from(onSelectionChange.mock.calls[0][0])).toEqual([
      "/beta.txt",
    ]);
  });

  it("does not scroll on focus changes (the virtualizer owns reveal)", () => {
    render(<Harness focusedIndex={1} />);

    expect(Element.prototype.scrollIntoView).not.toHaveBeenCalled();
  });

  describe("movement", () => {
    const press = (key: string, init: KeyboardEventInit = {}) =>
      fireEvent.keyDown(screen.getByTestId("files"), { key, ...init });
    const selected = (onSelectionChange: ReturnType<typeof vi.fn>) =>
      Array.from(onSelectionChange.mock.lastCall![0]);

    it("moves the focus and selection one item with ArrowDown and ArrowUp", () => {
      const onFocusChange = vi.fn();
      const onSelectionChange = vi.fn();
      render(
        <Harness
          focusedIndex={1}
          onFocusChange={onFocusChange}
          onSelectionChange={onSelectionChange}
        />,
      );

      press("ArrowDown");
      expect(onFocusChange).toHaveBeenLastCalledWith(2);
      expect(selected(onSelectionChange)).toEqual(["/another.txt"]);

      press("ArrowUp");
      expect(onFocusChange).toHaveBeenLastCalledWith(0);
      expect(selected(onSelectionChange)).toEqual(["/alpha.txt"]);
    });

    it("stops at the ends and starts from the edges when nothing is focused", () => {
      const onFocusChange = vi.fn();
      const { unmount } = render(
        <Harness focusedIndex={2} onFocusChange={onFocusChange} />,
      );
      press("ArrowDown");
      expect(onFocusChange).toHaveBeenLastCalledWith(2);
      unmount();

      const fresh = vi.fn();
      render(<Harness onFocusChange={fresh} />);
      press("ArrowDown");
      expect(fresh).toHaveBeenLastCalledWith(0);
      press("ArrowUp");
      expect(fresh).toHaveBeenLastCalledWith(2);
    });

    it("ignores ArrowLeft and ArrowRight in the one-column list", () => {
      const onFocusChange = vi.fn();
      render(<Harness focusedIndex={1} onFocusChange={onFocusChange} />);

      expect(press("ArrowRight")).toBe(true);
      press("ArrowLeft");

      expect(onFocusChange).not.toHaveBeenCalled();
    });

    it("moves by item with Left/Right and by row with Up/Down in a grid", () => {
      const onFocusChange = vi.fn();
      render(
        <Harness
          columnCount={3}
          focusedIndex={4}
          listItems={gridItems}
          onFocusChange={onFocusChange}
        />,
      );

      press("ArrowRight");
      expect(onFocusChange).toHaveBeenLastCalledWith(5);
      press("ArrowLeft");
      expect(onFocusChange).toHaveBeenLastCalledWith(3);
      press("ArrowDown");
      expect(onFocusChange).toHaveBeenLastCalledWith(6);
      press("ArrowUp");
      expect(onFocusChange).toHaveBeenLastCalledWith(1);
    });

    it("jumps to the first and last item with Home and End", () => {
      const onFocusChange = vi.fn();
      const onSelectionChange = vi.fn();
      render(
        <Harness
          focusedIndex={1}
          onFocusChange={onFocusChange}
          onSelectionChange={onSelectionChange}
        />,
      );

      press("End");
      expect(onFocusChange).toHaveBeenLastCalledWith(2);
      expect(selected(onSelectionChange)).toEqual(["/another.txt"]);
      press("Home");
      expect(onFocusChange).toHaveBeenLastCalledWith(0);
      expect(selected(onSelectionChange)).toEqual(["/alpha.txt"]);
    });

    it("extends a range from the anchor with Shift and replaces it as it shrinks", () => {
      const onSelectionChange = vi.fn();
      render(
        <Harness
          anchorIndex={1}
          focusedIndex={1}
          onSelectionChange={onSelectionChange}
        />,
      );

      press("ArrowDown", { shiftKey: true });
      expect(selected(onSelectionChange)).toEqual([
        "/beta.txt",
        "/another.txt",
      ]);

      press("Home", { shiftKey: true });
      expect(selected(onSelectionChange)).toEqual(["/alpha.txt", "/beta.txt"]);
    });

    it("leaves Ctrl+Arrow alone", () => {
      const onFocusChange = vi.fn();
      render(<Harness focusedIndex={0} onFocusChange={onFocusChange} />);

      expect(press("ArrowDown", { ctrlKey: true })).toBe(true);

      expect(onFocusChange).not.toHaveBeenCalled();
    });
  });

  describe("opening and leaving folders", () => {
    it("opens the focused folder with Enter", () => {
      const onOpenDirectory = vi.fn();
      const onDownloadFile = vi.fn();
      render(
        <Harness
          focusedIndex={0}
          listItems={gridItems}
          onDownloadFile={onDownloadFile}
          onOpenDirectory={onOpenDirectory}
        />,
      );

      fireEvent.keyDown(screen.getByTestId("files"), { key: "Enter" });

      expect(onOpenDirectory).toHaveBeenCalledWith("/photos/");
      expect(onDownloadFile).not.toHaveBeenCalled();
    });

    it("runs the file double-click action with Enter on a file", () => {
      const onOpenDirectory = vi.fn();
      const onDownloadFile = vi.fn();
      render(
        <Harness
          focusedIndex={1}
          listItems={gridItems}
          onDownloadFile={onDownloadFile}
          onOpenDirectory={onOpenDirectory}
        />,
      );

      fireEvent.keyDown(screen.getByTestId("files"), { key: "Enter" });

      expect(onDownloadFile).toHaveBeenCalledWith(gridItems[1]);
      expect(onOpenDirectory).not.toHaveBeenCalled();
    });

    it("does nothing on Enter without a focused item or on a focused button", () => {
      const onOpenDirectory = vi.fn();
      const { unmount } = render(
        <Harness listItems={gridItems} onOpenDirectory={onOpenDirectory} />,
      );
      fireEvent.keyDown(screen.getByTestId("files"), { key: "Enter" });
      unmount();

      render(
        <>
          <button type="button">Toolbar</button>
          <Harness
            focusedIndex={0}
            global
            listItems={gridItems}
            onOpenDirectory={onOpenDirectory}
          />
        </>,
      );
      fireEvent.keyDown(screen.getByRole("button", { name: "Toolbar" }), {
        key: "Enter",
      });

      expect(onOpenDirectory).not.toHaveBeenCalled();
    });

    it("goes to the parent folder with Backspace and Alt+ArrowUp", () => {
      const onNavigateUp = vi.fn();
      const onFocusChange = vi.fn();
      render(
        <Harness
          focusedIndex={1}
          onFocusChange={onFocusChange}
          onNavigateUp={onNavigateUp}
        />,
      );
      const container = screen.getByTestId("files");

      fireEvent.keyDown(container, { key: "Backspace" });
      fireEvent.keyDown(container, { altKey: true, key: "ArrowUp" });

      expect(onNavigateUp).toHaveBeenCalledTimes(2);
      expect(onFocusChange).not.toHaveBeenCalled();
    });

    it("keeps Backspace for inputs", () => {
      const onNavigateUp = vi.fn();
      render(<Harness onNavigateUp={onNavigateUp} />);

      fireEvent.keyDown(screen.getByLabelText("rename"), { key: "Backspace" });

      expect(onNavigateUp).not.toHaveBeenCalled();
    });
  });

  describe("typeahead", () => {
    beforeEach(() => {
      vi.useFakeTimers({ toFake: ["Date"] });
    });
    afterEach(() => {
      vi.useRealTimers();
    });

    const type = (key: string, init: KeyboardEventInit = {}) =>
      fireEvent.keyDown(screen.getByTestId("files"), { key, ...init });

    it("accumulates characters typed within a second into a prefix", () => {
      const onFocusChange = vi.fn();
      const onSelectionChange = vi.fn();
      render(
        <Harness
          focusedIndex={2}
          listItems={gridItems}
          onFocusChange={onFocusChange}
          onSelectionChange={onSelectionChange}
        />,
      );

      type("p");
      expect(onFocusChange).toHaveBeenLastCalledWith(0);
      vi.advanceTimersByTime(500);
      type("H");
      // "ph" matches photos first, searching from the focused index (2) wraps.
      expect(onFocusChange).toHaveBeenLastCalledWith(0);
      vi.advanceTimersByTime(200);
      type("o");
      type("n");
      expect(onFocusChange).toHaveBeenLastCalledWith(1);
      expect(Array.from(onSelectionChange.mock.lastCall![0])).toEqual([
        "/phone.txt",
      ]);
    });

    it("starts a new prefix after a second of silence", () => {
      const onFocusChange = vi.fn();
      render(
        <Harness
          focusedIndex={0}
          listItems={gridItems}
          onFocusChange={onFocusChange}
        />,
      );

      type("p");
      vi.advanceTimersByTime(1000);
      type("a");

      expect(onFocusChange).toHaveBeenLastCalledWith(2);
    });

    it("steps past the focused item for a single character and wraps", () => {
      const onFocusChange = vi.fn();
      render(
        <Harness
          focusedIndex={0}
          listItems={gridItems}
          onFocusChange={onFocusChange}
        />,
      );

      type("p");

      expect(onFocusChange).toHaveBeenLastCalledWith(1);
    });

    it("types digits but not modified keys or punctuation", () => {
      const onFocusChange = vi.fn();
      const list: FileItem[] = [
        { name: "1.txt", path: "/1.txt", type: "file" },
        { name: "2.txt", path: "/2.txt", type: "file" },
      ];
      render(<Harness listItems={list} onFocusChange={onFocusChange} />);

      type("2");
      expect(onFocusChange).toHaveBeenLastCalledWith(1);
      onFocusChange.mockClear();
      vi.advanceTimersByTime(1000);
      type("1", { altKey: true });
      type("1", { metaKey: true });
      type(".");

      expect(onFocusChange).not.toHaveBeenCalled();
    });
  });
});
