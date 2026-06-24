import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";

import { useTabUrlState } from "@/hooks/useTabUrlState";
import { act, renderHook } from "@/test/render";

describe("useTabUrlState", () => {
  it("uses the default tab when the URL param is absent", () => {
    const { result } = renderHook(() => useTabUrlState("overview"), {
      wrapper: ({ children }: { children: ReactNode }) => (
        <MemoryRouter>{children}</MemoryRouter>
      ),
    });

    expect(result.current[0]).toBe("overview");
  });

  it("reads and updates the configured URL param", () => {
    const { result } = renderHook(() => useTabUrlState("overview", "view"), {
      wrapper: ({ children }: { children: ReactNode }) => (
        <MemoryRouter initialEntries={["/storage?view=details&keep=yes"]}>
          {children}
        </MemoryRouter>
      ),
    });

    expect(result.current[0]).toBe("details");
    act(() => result.current[1]("settings"));
    expect(result.current[0]).toBe("settings");
  });
});
