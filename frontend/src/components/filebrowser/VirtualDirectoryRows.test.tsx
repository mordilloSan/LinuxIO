import { describe, expect, it, vi } from "vitest";

import type { FileItem } from "@/types/filebrowser";

const rowRender = vi.hoisted(() => vi.fn());

vi.mock("@/components/filebrowser/FileListRow", () => ({
  default: ({ path, selected }: { path: string; selected: boolean }) => {
    rowRender(path);
    return <div>{`${path}:${selected ? "selected" : "idle"}`}</div>;
  },
}));

vi.mock("@/components/cards/FileCard", () => ({
  default: ({ path, selected }: { path: string; selected: boolean }) => {
    rowRender(path);
    return <div>{`${path}:${selected ? "selected" : "idle"}`}</div>;
  },
}));

const { DirectoryItem } =
  await import("@/components/filebrowser/VirtualDirectoryRows");
const { render, screen } = await import("@/test/render");

const firstItem: FileItem = {
  name: "first.txt",
  path: "/files/first.txt",
  type: "file",
};
const secondItem: FileItem = {
  name: "second.txt",
  path: "/files/second.txt",
  type: "file",
};
const noop = () => {};
const noopFile = (_item: FileItem) => {};
const noopItemEvent = () => {};
const noopRename = () => {};

function Item({ item, selected }: { item: FileItem; selected: boolean }) {
  return (
    <DirectoryItem
      disableHover={false}
      isCut={false}
      isLoadingSubfolders={false}
      isRenaming={false}
      item={item}
      itemKind="file"
      onCancelRename={noop}
      onConfirmRename={noopRename}
      onDownloadFile={noopFile}
      onFileClick={noopItemEvent}
      onFileContextMenu={noopItemEvent}
      onFolderClick={noopItemEvent}
      onFolderContextMenu={noopItemEvent}
      onOpenDirectory={noop}
      selected={selected}
      viewMode="list"
    />
  );
}

describe("VirtualDirectoryItem", () => {
  it("rerenders only the item whose derived selection state changed", () => {
    const view = render(
      <>
        <Item item={firstItem} selected={false} />
        <Item item={secondItem} selected={false} />
      </>,
    );

    expect(rowRender).toHaveBeenCalledTimes(2);

    view.rerender(
      <>
        <Item item={firstItem} selected={false} />
        <Item item={secondItem} selected />
      </>,
    );

    expect(screen.getByText("/files/second.txt:selected")).toBeInTheDocument();
    expect(
      rowRender.mock.calls.filter(([path]) => path === firstItem.path),
    ).toHaveLength(1);
    expect(
      rowRender.mock.calls.filter(([path]) => path === secondItem.path),
    ).toHaveLength(2);
  });
});
