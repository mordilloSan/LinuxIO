import { describe, expect, it, vi } from "vitest";

import { useRegisterCreateHandler } from "@/hooks/useRegisterCreateHandler";
import { renderHook } from "@/test/render";

describe("useRegisterCreateHandler", () => {
  it("registers once while invoking the latest handler", () => {
    const register = vi.fn<(handler: () => void) => void>();
    const initialHandler = vi.fn();
    const latestHandler = vi.fn();
    const { rerender } = renderHook(
      ({ handler }: { handler: () => void }) =>
        useRegisterCreateHandler(register, handler),
      { initialProps: { handler: initialHandler } },
    );

    expect(register).toHaveBeenCalledTimes(1);
    const registeredHandler = register.mock.calls[0][0];
    registeredHandler();
    expect(initialHandler).toHaveBeenCalledTimes(1);

    rerender({ handler: latestHandler });

    expect(register).toHaveBeenCalledTimes(1);
    registeredHandler();
    expect(initialHandler).toHaveBeenCalledTimes(1);
    expect(latestHandler).toHaveBeenCalledTimes(1);
  });
});
