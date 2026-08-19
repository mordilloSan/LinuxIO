import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import type { ExtendedFileInfo } from "@/api";
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
    const queryKey = ["filebrowser", "listing-identity"] as const;
    const initial: ExtendedFileInfo = {
      files: [
        {
          hasPreview: false,
          hidden: false,
          modified: "2026-08-19T10:00:00Z",
          name: "stable.txt",
          size: 1,
          symlink: false,
          type: "file",
        },
        {
          hasPreview: false,
          hidden: false,
          modified: "2026-08-19T10:00:00Z",
          name: "changed.txt",
          size: 1,
          symlink: false,
          type: "file",
        },
      ],
      folders: [],
      hasPreview: false,
      hidden: false,
      modified: "2026-08-19T10:00:00Z",
      name: "files",
      path: "/files/",
      size: 2,
      symlink: false,
      type: "directory",
    };
    queryClient.setQueryData(queryKey, initial);
    const listingQueryOptions = {
      queryFn: async () => initial,
      queryKey,
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
  });
});
