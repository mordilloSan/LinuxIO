import { useState } from "react";
import { describe, expect, it } from "vitest";

import { useFileBrowserSearchAndSort } from "@/hooks/filebrowser/useFileBrowserSearchAndSort";
import { act, renderHook } from "@/test/render";
import type { SortField, SortOrder } from "@/types/filebrowser";

// Drives the hook against real sort state so the toggle logic is exercised
// end-to-end rather than through inspected updater callbacks.
function useHarness() {
  const [sortField, setSortField] = useState<SortField>("name");
  const [sortOrder, setSortOrder] = useState<SortOrder>("asc");
  const api = useFileBrowserSearchAndSort({ setSortField, setSortOrder });
  return { ...api, sortField, sortOrder };
}

describe("useFileBrowserSearchAndSort", () => {
  it("starts with an empty query and updates it on change", () => {
    const { result } = renderHook(() => useHarness());

    expect(result.current.searchQuery).toBe("");

    act(() => result.current.handleSearchChange("readme"));

    expect(result.current.searchQuery).toBe("readme");
  });

  it("clears the query when the directory path changes", () => {
    const { result } = renderHook(() => useHarness());

    act(() => result.current.handleSearchChange("readme"));
    act(() => result.current.handlePathChange());

    expect(result.current.searchQuery).toBe("");
  });

  it("exposes a direct setter for the query", () => {
    const { result } = renderHook(() => useHarness());

    act(() => result.current.setSearchQuery("logs"));

    expect(result.current.searchQuery).toBe("logs");
  });

  it("toggles the sort order when the active field is selected again", () => {
    const { result } = renderHook(() => useHarness());

    act(() => result.current.handleSortChange("name"));
    expect(result.current.sortField).toBe("name");
    expect(result.current.sortOrder).toBe("desc");

    act(() => result.current.handleSortChange("name"));
    expect(result.current.sortOrder).toBe("asc");
  });

  it("switches field and resets order to ascending for a new field", () => {
    const { result } = renderHook(() => useHarness());

    act(() => result.current.handleSortChange("name")); // -> desc
    act(() => result.current.handleSortChange("size")); // new field -> asc

    expect(result.current.sortField).toBe("size");
    expect(result.current.sortOrder).toBe("asc");
  });
});
