import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { linuxio, type DirectoryListing } from "@/api";
import { normalizeResource } from "@/components/filebrowser/utils";
import type { FileBrowserListingQueryOptions } from "@/hooks/filebrowser/fileBrowserListingQueryOptions";
import { useFileQueries } from "@/hooks/filebrowser/useFileQueries";
import { act, createTestQueryClient, renderHook, waitFor } from "@/test/render";

vi.mock("@/hooks/filebrowser/useFileMultipleDirectoryDetails", () => ({
  useFileMultipleDirectoryDetails: () => ({
    isAnyError: false,
    isAnyLoading: false,
    items: [],
    totalSize: 0,
  }),
}));

describe("useFileQueries", () => {
  it("structurally shares unchanged normalized directory items", async () => {
    const queryClient = createTestQueryClient();
    const queryKey = linuxio.filebrowser.list_directory({
      path: "/files",
    }).queryKey;
    const initial: DirectoryListing = {
      files: [
        {
          canOpenAsText: true,
          isRegularFile: true,
          modified: "2026-08-19T10:00:00Z",
          name: "stable.txt",
          size: 1,
          symlink: false,
        },
        {
          canOpenAsText: true,
          isRegularFile: true,
          modified: "2026-08-19T10:00:00Z",
          name: "changed.txt",
          size: 1,
          symlink: false,
        },
      ],
      folders: [],
    };
    queryClient.setQueryData(queryKey, initial);
    const listingQueryOptions = {
      queryFn: async () => initial,
      queryKey,
      select: (data: DirectoryListing) => normalizeResource(data, "/files"),
      staleTime: Infinity,
    } as unknown as FileBrowserListingQueryOptions;
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
    const { result } = renderHook(
      () =>
        useFileQueries({
          detailTarget: null,
          editingPath: null,
          hasMultipleDetailTargets: false,
          hasSingleDetailTarget: false,
          listingQueryOptions,
        }),
      { wrapper },
    );
    const stableItem = result.current.resource.items?.[0];
    const changedItem = result.current.resource.items?.[1];
    expect(stableItem).toBeDefined();
    expect(changedItem).toBeDefined();

    act(() => {
      queryClient.setQueryData(queryKey, {
        ...initial,
        files: [
          initial.files[0],
          {
            ...initial.files[1],
            modified: "2026-08-19T11:00:00Z",
          },
        ],
      });
    });

    await waitFor(() => {
      expect(result.current.resource.items?.[1]?.modTime).toBe(
        "2026-08-19T11:00:00Z",
      );
    });
    expect(result.current.resource.items?.[0]).toBe(stableItem);
    expect(result.current.resource.items?.[1]).not.toBe(changedItem);
    const queryRoutes = queryClient
      .getQueryCache()
      .getAll()
      .map(({ queryKey }) => queryKey[2]);
    expect(queryRoutes).toContain("list_directory");
    expect(queryRoutes).not.toContain("resource_get");
  });

  it("reuses selected items for single and multi-details without metadata listings", () => {
    const queryClient = createTestQueryClient();
    const path = "/files";
    const listing: DirectoryListing = {
      files: [
        {
          canOpenAsText: true,
          isRegularFile: true,
          modified: "2026-08-19T10:00:00Z",
          name: "stable.txt",
          size: 1,
          symlink: false,
        },
        {
          canOpenAsText: true,
          isRegularFile: true,
          modified: "2026-08-19T10:00:00Z",
          name: "other.txt",
          size: 1,
          symlink: false,
        },
      ],
      folders: [],
    };
    const queryKey = linuxio.filebrowser.list_directory({ path }).queryKey;
    queryClient.setQueryData(queryKey, listing);
    const listingQueryOptions = {
      queryFn: async () => listing,
      queryKey,
      select: (data: DirectoryListing) => normalizeResource(data, path),
      staleTime: Infinity,
    } as unknown as FileBrowserListingQueryOptions;
    const detailItems = normalizeResource(listing, path).items ?? [];
    const singlePath = detailItems[0]?.path;
    if (!singlePath) throw new Error("Expected a selected file");
    queryClient.setQueryData(
      linuxio.filebrowser.resource_stat({ path: singlePath }).queryKey,
      { group: "users", mode: "0644", owner: "root", permissions: "644" },
    );
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );

    const single = renderHook(
      () =>
        useFileQueries({
          detailItems,
          detailTarget: [singlePath],
          editingPath: null,
          hasMultipleDetailTargets: false,
          hasSingleDetailTarget: true,
          listingQueryOptions,
        }),
      { wrapper },
    );
    expect(single.result.current.detailResource).toBe(detailItems[0]);

    renderHook(
      () =>
        useFileQueries({
          detailItems,
          detailTarget: detailItems.map((item) => item.path),
          editingPath: null,
          hasMultipleDetailTargets: true,
          hasSingleDetailTarget: false,
          listingQueryOptions,
        }),
      { wrapper },
    );

    expect(
      queryClient
        .getQueryCache()
        .getAll()
        .map(({ queryKey }) => queryKey[2]),
    ).not.toContain("resource_get");
  });
});
