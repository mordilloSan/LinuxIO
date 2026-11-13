import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { ConfigContext } from "@/contexts/ConfigContext";
import {
  createIsSectionOpen,
  type SectionPage,
  toggleSection,
  useSectionToggles,
} from "@/hooks/useSectionToggles";
import {
  act,
  createConfigContextValue,
  createTestQueryClient,
  renderHook,
  seedConfigCache,
} from "@/test/render";

const PAGE = {
  key: "page",
  sections: ["first", "second", "third"],
} as const satisfies SectionPage<string>;

describe("section toggles", () => {
  it("opens every section of a page with nothing stored", () => {
    const isOpen = createIsSectionOpen({ other: ["first"] }, PAGE);

    expect(PAGE.sections.every(isOpen)).toBe(true);
  });

  it("reads a stored name as collapsed", () => {
    const isOpen = createIsSectionOpen({ page: ["second"] }, PAGE);

    expect(isOpen("first")).toBe(true);
    expect(isOpen("second")).toBe(false);
    // @ts-expect-error -- a name outside the page's tuple does not type-check
    expect(isOpen("fourth")).toBe(true);
  });

  it("collapses in page order and leaves other pages alone", () => {
    const collapsed = toggleSection(
      { other: ["x"], page: ["third"] },
      PAGE,
      "first",
    );

    expect(collapsed).toEqual({ other: ["x"], page: ["first", "third"] });
  });

  it("drops a page once nothing on it is collapsed", () => {
    expect(toggleSection({ page: ["second"] }, PAGE, "second")).toEqual({});
  });

  it("drops stored names the page no longer renders", () => {
    expect(
      toggleSection({ page: ["removed", "third"] }, PAGE, "second"),
    ).toEqual({ page: ["second", "third"] });
  });

  it("reads and writes the collapsedSections preference", () => {
    const setKey = vi.fn();
    const queryClient = createTestQueryClient();
    seedConfigCache(queryClient, { collapsedSections: { page: ["first"] } });
    const value = createConfigContextValue({ setKey });
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>
        <ConfigContext.Provider value={value}>
          {children}
        </ConfigContext.Provider>
      </QueryClientProvider>
    );

    const { result } = renderHook(() => useSectionToggles(PAGE), { wrapper });
    expect(result.current.isOpen("first")).toBe(false);
    expect(result.current.isOpen("second")).toBe(true);

    act(() => result.current.toggle("first"));
    expect(setKey).toHaveBeenCalledWith(
      "collapsedSections",
      expect.any(Function),
    );
    const update = setKey.mock.calls[0]?.[1] as (
      prev: Record<string, string[]>,
    ) => Record<string, string[]>;
    expect(update({ page: ["first"] })).toEqual({});
  });
});
