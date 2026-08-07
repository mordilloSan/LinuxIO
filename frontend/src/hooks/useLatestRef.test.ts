import { describe, expect, it } from "vitest";

import { useLatestRef } from "@/hooks/useLatestRef";
import { renderHook } from "@/test/render";

describe("useLatestRef", () => {
  it("keeps a stable ref and updates it after rerender", () => {
    const { result, rerender } = renderHook(
      ({ value }: { value: string }) => useLatestRef(value),
      { initialProps: { value: "initial" } },
    );
    const ref = result.current;

    expect(ref.current).toBe("initial");

    rerender({ value: "latest" });

    expect(result.current).toBe(ref);
    expect(result.current.current).toBe("latest");
  });
});
