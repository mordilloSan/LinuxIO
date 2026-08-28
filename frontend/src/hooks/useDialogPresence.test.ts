import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { useDialogPresence } from "./useDialogPresence";

describe("useDialogPresence", () => {
  it("retains dialog content until its exit completes", () => {
    const { result, rerender } = renderHook(
      ({ value }: { value: string | null }) => useDialogPresence<string>(value),
      { initialProps: { value: null as string | null } },
    );

    rerender({ value: "dialog payload" });
    expect(result.current.content).toBe("dialog payload");

    rerender({ value: null });
    expect(result.current.content).toBe("dialog payload");

    act(() => result.current.onExited());
    expect(result.current.content).toBeNull();
  });
});
