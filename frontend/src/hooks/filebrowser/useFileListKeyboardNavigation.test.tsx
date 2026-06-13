import React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useFileListKeyboardNavigation } from "@/hooks/filebrowser/useFileListKeyboardNavigation";
import { fireEvent, render, screen } from "@/test/render";
import type { FileItem } from "@/types/filebrowser";

const items: FileItem[] = [
  { name: "alpha.txt", path: "/alpha.txt", type: "file" },
  { name: "beta.txt", path: "/beta.txt", type: "file" },
  { name: "another.txt", path: "/another.txt", type: "file" },
];

function Harness({
  focusedIndex = -1,
  global = false,
  onDelete = vi.fn(),
  onFocusChange = vi.fn(),
  onRename = vi.fn(),
  onSelectionChange = vi.fn(),
}: {
  focusedIndex?: number;
  global?: boolean;
  onDelete?: () => void;
  onFocusChange?: (index: number) => void;
  onRename?: () => void;
  onSelectionChange?: (paths: Set<string>) => void;
}) {
  const containerRef = React.useRef<HTMLDivElement>(null);

  useFileListKeyboardNavigation({
    allItems: items,
    containerRef: containerRef as React.RefObject<HTMLDivElement>,
    focusedIndex,
    global,
    onDelete,
    onFocusChange,
    onRename,
    onSelectionChange,
    selectedPaths: new Set(),
  });

  return (
    <div ref={containerRef} data-testid="files">
      {items.map((item) => (
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

  it("does not intercept keyboard shortcuts while typing in inputs", () => {
    const onSelectionChange = vi.fn();

    render(<Harness onSelectionChange={onSelectionChange} />);
    fireEvent.keyDown(screen.getByLabelText("rename"), {
      key: "a",
      ctrlKey: true,
    });

    expect(onSelectionChange).not.toHaveBeenCalled();
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

  it("scrolls focused cards into view", () => {
    render(<Harness focusedIndex={1} />);

    expect(Element.prototype.scrollIntoView).toHaveBeenCalledWith({
      behavior: "smooth",
      block: "nearest",
      inline: "nearest",
    });
  });
});
