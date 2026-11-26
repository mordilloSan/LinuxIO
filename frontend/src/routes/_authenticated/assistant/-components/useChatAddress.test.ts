import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { assistantStore } from "./assistant-store";
import { useChatAddress } from "./useChatAddress";

const navigate = vi.hoisted(() => vi.fn());
vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-router")>()),
  useNavigate: () => navigate,
}));

beforeEach(() => {
  navigate.mockReset();
  assistantStore.disconnect();
});

describe("useChatAddress", () => {
  it("puts the open chat in the address in place of the entry", () => {
    const { rerender } = renderHook(({ chat }) => useChatAddress(chat), {
      initialProps: { chat: "s1" },
    });
    // Nothing is open yet: the address keeps the chat it names.
    expect(navigate).not.toHaveBeenCalled();

    act(() => assistantStore._patch({ status: "ready", sessionId: "s1" }));
    expect(navigate).not.toHaveBeenCalled();

    // Another chat opened (the Chats card, New chat, a delete).
    act(() => assistantStore._patch({ sessionId: "s2" }));
    expect(navigate).toHaveBeenCalledExactlyOnceWith({
      to: "/assistant",
      search: { chat: "s2" },
      replace: true,
      resetScroll: false,
    });

    rerender({ chat: "s2" });
    expect(navigate).toHaveBeenCalledTimes(1);
  });

  it("names the open chat when the address names none", () => {
    act(() => assistantStore._patch({ status: "ready", sessionId: "s1" }));
    renderHook(() => useChatAddress(undefined));
    expect(navigate).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ search: { chat: "s1" } }),
    );
  });
});
