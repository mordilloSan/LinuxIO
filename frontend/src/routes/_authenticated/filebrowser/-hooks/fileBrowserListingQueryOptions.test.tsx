import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { act, createTestQueryClient, renderHook, waitFor } from "@/test/render";

import { createFileBrowserListingQueryOptions } from "./fileBrowserListingQueryOptions";
import { useFileDirectorySize } from "./useFileDirectorySize";
import { useFileSubfolders } from "./useFileSubfolders";

const { dirSize, listDirectory, subfolders } = vi.hoisted(() => ({
  dirSize: vi.fn(),
  listDirectory: vi.fn(),
  subfolders: vi.fn(),
}));

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
        list_directory: ({ path }: { path: string }) => ({
          queryFn: () => listDirectory(path),
          queryKey: ["linuxio", "filebrowser", "list_directory", { path }],
        }),
        subfolders: ({ path }: { path: string }) => ({
          queryFn: () => subfolders(path),
          queryKey: ["linuxio", "filebrowser", "subfolders", { path }],
        }),
      },
    },
  };
});

function renderSizesOf(path: string, queryClient = createTestQueryClient()) {
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  const view = renderHook(
    () => ({
      folder: useFileDirectorySize(path),
      subfolders: useFileSubfolders(path),
    }),
    { wrapper },
  );
  return { ...view, queryClient };
}

// What a visit to a folder does: the bridge reads it from disk and tells the
// indexer to bring its rows up to date.
const readListing = (
  queryClient: ReturnType<typeof createTestQueryClient>,
  path: string,
) =>
  act(async () => {
    await queryClient.query({
      ...createFileBrowserListingQueryOptions(path),
      staleTime: 0,
    });
  });

describe("createFileBrowserListingQueryOptions", () => {
  beforeEach(() => {
    listDirectory.mockReset().mockResolvedValue({ items: [] });
    dirSize
      .mockReset()
      .mockResolvedValue({ fileCount: 1, folderCount: 1, size: 10 });
    subfolders.mockReset().mockResolvedValue({ subfolders: [] });
  });

  it("asks for a folder's sizes again when the folder is read again", async () => {
    const { result, queryClient } = renderSizesOf("/srv/data");
    await waitFor(() => expect(result.current.folder.size).toBe(10));
    expect(dirSize).toHaveBeenCalledTimes(1);
    expect(subfolders).toHaveBeenCalledTimes(1);

    // Changed from a terminal since: the sizes the page holds are minutes
    // from being stale by their own clock.
    dirSize.mockResolvedValue({ fileCount: 2, folderCount: 1, size: 30 });
    await readListing(queryClient, "/srv/data");

    await waitFor(() => expect(result.current.folder.size).toBe(30));
    expect(dirSize).toHaveBeenCalledTimes(2);
    expect(subfolders).toHaveBeenCalledTimes(2);
  });

  // Coming back to the file browser from another page: the route's loader
  // reads the folder before the page, and with it the size hooks, exists.
  it("asks again on the next visit when the folder was read while nothing showed its sizes", async () => {
    const first = renderSizesOf("/srv/data");
    await waitFor(() => expect(first.result.current.folder.size).toBe(10));
    first.unmount();

    dirSize.mockResolvedValue({ fileCount: 2, folderCount: 1, size: 30 });
    await readListing(first.queryClient, "/srv/data");
    expect(dirSize).toHaveBeenCalledTimes(1);

    const second = renderSizesOf("/srv/data", first.queryClient);
    await waitFor(() => expect(second.result.current.folder.size).toBe(30));
    expect(dirSize).toHaveBeenCalledTimes(2);
    expect(subfolders).toHaveBeenCalledTimes(2);
  });

  it("keeps the sizes on the next visit when the folder was not read again", async () => {
    const first = renderSizesOf("/srv/data");
    await waitFor(() => expect(first.result.current.folder.size).toBe(10));
    first.unmount();

    const second = renderSizesOf("/srv/data", first.queryClient);
    expect(second.result.current.folder.size).toBe(10);
    await act(async () => {
      await Promise.resolve();
    });
    expect(dirSize).toHaveBeenCalledTimes(1);
    expect(subfolders).toHaveBeenCalledTimes(1);
  });

  it("keeps the sizes of a folder that was not read", async () => {
    const { result, queryClient } = renderSizesOf("/srv/data");
    await waitFor(() => expect(result.current.folder.size).toBe(10));

    await readListing(queryClient, "/srv/other");
    await readListing(queryClient, "/srv");

    expect(listDirectory).toHaveBeenCalledTimes(2);
    expect(dirSize).toHaveBeenCalledTimes(1);
    expect(subfolders).toHaveBeenCalledTimes(1);
  });

  it("hands on the listing the bridge sent, for the path without its slash", async () => {
    const queryClient = createTestQueryClient();
    listDirectory.mockResolvedValue({
      files: [],
      folders: [{ name: "docs" }],
    });

    const data = await queryClient.query({
      ...createFileBrowserListingQueryOptions("/srv/data/"),
      staleTime: 0,
    });

    expect(listDirectory).toHaveBeenCalledWith("/srv/data");
    expect(data.path).toBe("/srv/data");
    expect(data.items).toMatchObject([{ name: "docs", type: "directory" }]);
  });
});
