import { useSearch } from "@tanstack/react-router";
import { describe, expect, it } from "vitest";

import { useTabUrlState } from "@/hooks/useTabUrlState";
import {
  act,
  createTanStackRouterWrapper,
  renderHook,
  waitFor,
} from "@/test/render";

describe("useTabUrlState", () => {
  it("uses the default tab when the URL param is absent", async () => {
    const { result } = renderHook(
      () => useTabUrlState("overview", "storageTab"),
      {
        wrapper: createTanStackRouterWrapper().Wrapper,
      },
    );

    await waitFor(() => expect(result.current?.[0]).toBe("overview"));
  });

  it("uses the default tab when the configured URL param is empty", async () => {
    const { result } = renderHook(
      () => useTabUrlState("overview", "storageTab"),
      {
        wrapper: createTanStackRouterWrapper({
          initialEntries: ["/storage?storageTab="],
        }).Wrapper,
      },
    );

    await waitFor(() => expect(result.current?.[0]).toBe("overview"));
  });

  it("reads and updates the configured URL param", async () => {
    const { result } = renderHook(
      () => useTabUrlState("overview", "storageTab"),
      {
        wrapper: createTanStackRouterWrapper({
          initialEntries: ["/storage?storageTab=details&keep=yes"],
        }).Wrapper,
      },
    );

    await waitFor(() => expect(result.current?.[0]).toBe("details"));
    act(() => result.current?.[1]("settings"));
    await waitFor(() => expect(result.current?.[0]).toBe("settings"));
  });

  it("preserves sibling query params when switching tabs (Accounts deep-link contract)", async () => {
    // Mirrors the SystemHealth -> Accounts deep link: the tab switch must not
    // drop focusLoginEventId/failedLoginAlertId, which UserAccountDetails reads.
    const { result } = renderHook(
      () => ({
        tab: useTabUrlState("users", "accountsTab"),
        search: useSearch({ strict: false }),
      }),
      {
        wrapper: createTanStackRouterWrapper({
          initialEntries: [
            "/accounts?accountsTab=users&focusLoginEventId=evt-42&failedLoginAlertId=alert-7",
          ],
        }).Wrapper,
      },
    );

    await waitFor(() => expect(result.current?.tab[0]).toBe("users"));

    act(() => result.current?.tab[1]("groups"));

    await waitFor(() => {
      expect(result.current?.search.accountsTab).toBe("groups");
      expect(result.current?.search.focusLoginEventId).toBe("evt-42");
      expect(result.current?.search.failedLoginAlertId).toBe("alert-7");
    });
  });
});
