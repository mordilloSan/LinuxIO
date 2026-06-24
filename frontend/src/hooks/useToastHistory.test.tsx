import { describe, expect, it, vi } from "vitest";

import {
  ToastHistoryContext,
  type ToastHistoryContextValue,
} from "@/contexts/ToastContext";
import { useClearToastHistory, useToastHistory } from "@/hooks/useToastHistory";
import { act, renderHook } from "@/test/render";

const value: ToastHistoryContextValue = {
  clearHistory: vi.fn(),
  history: [
    { createdAt: 3, id: "3", title: "Third" },
    { createdAt: 2, id: "2", title: "Second" },
    { createdAt: 1, id: "1", title: "First" },
  ],
};

function wrapper({ children }: { children: React.ReactNode }) {
  return (
    <ToastHistoryContext.Provider value={value}>
      {children}
    </ToastHistoryContext.Provider>
  );
}

describe("useToastHistory", () => {
  it("throws outside ToastProvider", () => {
    expect(() => renderHook(() => useToastHistory())).toThrow(
      "useToastHistory must be used within ToastProvider",
    );
    expect(() => renderHook(() => useClearToastHistory())).toThrow(
      "useClearToastHistory must be used within ToastProvider",
    );
  });

  it("returns history limited to the requested size", () => {
    const { result } = renderHook(() => useToastHistory(2), { wrapper });

    expect(result.current.map((item) => item.title)).toEqual([
      "Third",
      "Second",
    ]);
  });

  it("returns the clear history callback", () => {
    const { result } = renderHook(() => useClearToastHistory(), { wrapper });

    act(() => result.current());

    expect(value.clearHistory).toHaveBeenCalledTimes(1);
  });
});
