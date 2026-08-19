import { describe, expect, it, vi } from "vitest";

import { HistoryHoverStore } from "./HistoryCard";

describe("HistoryHoverStore", () => {
  it("notifies subscribers only when the synchronized timestamp changes", () => {
    const store = new HistoryHoverStore();
    const first = vi.fn();
    const second = vi.fn();
    const unsubscribeFirst = store.subscribe(first);
    store.subscribe(second);

    store.setHoverTime(123);
    store.setHoverTime(123);
    expect(store.getSnapshot()).toBe(123);
    expect(first).toHaveBeenCalledTimes(1);
    expect(second).toHaveBeenCalledTimes(1);

    unsubscribeFirst();
    store.setHoverTime(null);
    expect(store.getSnapshot()).toBeNull();
    expect(first).toHaveBeenCalledTimes(1);
    expect(second).toHaveBeenCalledTimes(2);
  });
});
