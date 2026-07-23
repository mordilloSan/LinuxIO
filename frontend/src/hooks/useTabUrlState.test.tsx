import type { ReactNode } from "react";
import { MemoryRouter, useSearchParams } from "react-router-dom";
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

  it("uses the default tab when the configured URL param is empty", () => {
    const { result } = renderHook(() => useTabUrlState("overview"), {
      wrapper: ({ children }: { children: ReactNode }) => (
        <MemoryRouter initialEntries={["/storage?tab="]}>
          {children}
        </MemoryRouter>
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

  it("preserves sibling query params when switching tabs (Accounts deep-link contract)", () => {
    // Mirrors the SystemHealth -> Accounts deep link: the tab switch must not
    // drop focusLoginEventId/failedLoginAlertId, which UserAccountDetails reads.
    const { result } = renderHook(
      () => ({
        tab: useTabUrlState("users", "accountsTab"),
        search: useSearchParams(),
      }),
      {
        wrapper: ({ children }: { children: ReactNode }) => (
          <MemoryRouter
            initialEntries={[
              "/accounts?accountsTab=users&focusLoginEventId=evt-42&failedLoginAlertId=alert-7",
            ]}
          >
            {children}
          </MemoryRouter>
        ),
      },
    );

    expect(result.current.tab[0]).toBe("users");

    act(() => result.current.tab[1]("groups"));

    const params = result.current.search[0];
    expect(params.get("accountsTab")).toBe("groups");
    expect(params.get("focusLoginEventId")).toBe("evt-42");
    expect(params.get("failedLoginAlertId")).toBe("alert-7");
  });
});
