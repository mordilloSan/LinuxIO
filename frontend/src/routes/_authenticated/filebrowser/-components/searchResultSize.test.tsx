import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";
import { formatFileSize } from "@/utils/formatters";

import type { FileItem } from "../-types";
import { DirectoryItem } from "./DirectoryRows";

const dirSize = vi.hoisted(() => vi.fn());

vi.mock("@/hooks/useAuth", () => ({
  default: () => ({ privileged: true }),
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      filebrowser: {
        ...actual.linuxio.filebrowser,
        dir_size: ({ path }: { path: string }) => ({
          queryFn: () => dirSize(path),
          queryKey: ["linuxio", "filebrowser", "dir_size", { path }],
        }),
      },
    },
  };
});

const noop = () => {};

// A folder among the results of a search: the search answer carries the size
// the index holds for it.
const foundFolder: FileItem = {
  name: "home",
  path: "/home",
  showFullPath: true,
  size: 3 * 1024 * 1024,
  type: "directory",
};

function Found({
  isLoadingSubfolders = false,
  viewMode,
}: {
  isLoadingSubfolders?: boolean;
  viewMode: "card" | "list";
}) {
  return (
    <DirectoryItem
      disableHover={false}
      isCut={false}
      isLoadingSubfolders={isLoadingSubfolders}
      isRenaming={false}
      item={foundFolder}
      itemKind="folder"
      onCancelRename={noop}
      onConfirmRename={noop}
      onDownloadFile={noop}
      onFileClick={noop}
      onFileContextMenu={noop}
      onFolderClick={noop}
      onFolderContextMenu={noop}
      onOpenDirectory={noop}
      selected={false}
      viewMode={viewMode}
    />
  );
}

describe("a folder among search results", () => {
  beforeEach(() => {
    dirSize.mockReset().mockResolvedValue({
      fileCount: 1,
      folderCount: 1,
      size: 1,
    });
  });

  // The request it used to make counts every row below the folder, to fill
  // in a size the result already has.
  it.each(["list", "card"] as const)(
    "shows the size the search found, without asking for it again (%s)",
    async (viewMode) => {
      render(<Found viewMode={viewMode} />);

      expect(
        await screen.findByText(formatFileSize(foundFolder.size ?? 0, 1, "")),
      ).toBeInTheDocument();
      expect(dirSize).not.toHaveBeenCalled();
    },
  );

  it("does not wait for the sizes of the folder the search started in", () => {
    render(<Found isLoadingSubfolders viewMode="list" />);

    expect(
      screen.getByText(formatFileSize(foundFolder.size ?? 0, 1, "")),
    ).toBeInTheDocument();
  });
});
