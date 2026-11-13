import {
  focusManager,
  onlineManager,
  QueryClientProvider,
} from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterContextProvider,
} from "@tanstack/react-router";
import { act, renderHook, waitFor } from "@testing-library/react";
import { type ReactNode, StrictMode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  linuxio,
  type Alert,
  type AlertList,
  type TaskHistory,
  type TaskHistoryEntry,
  type TaskHostStatus,
  type TaskSnapshot,
} from "@/api";
import { bridgeConfigQueryKey } from "@/api/config-query";
import { AuthContext } from "@/contexts/AuthContext";
import { ConfigContext } from "@/contexts/ConfigContext";
import {
  buildTestConfigSnapshots,
  createAuthContextValue,
  createConfigContextValue,
  createTestQueryClient,
} from "@/test/render";
import { hasFreshSignIn, markFreshSignIn } from "@/utils/freshSignIn";

const mocks = vi.hoisted(() => ({
  call: vi.fn(),
  mutate: vi.fn(),
  toast: {
    error: vi.fn(),
    info: vi.fn(),
    success: vi.fn(),
    warning: vi.fn(),
  },
}));

vi.mock("@/api/calls", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/calls")>()),
  call: mocks.call,
}));

// Mutations travel over the stream, so their hooks are answered by route.
vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  const { mockMutationHooks } = await import("@/test/mutationHooks");
  return {
    ...actual,
    linuxio: mockMutationHooks(actual.linuxio, (endpoint) => ({
      mutate: (request: unknown) => mocks.mutate(endpoint.route, request),
    })),
  };
});

vi.mock("sonner", () => ({ toast: mocks.toast }));

const { subscribeNotificationsPanel } =
  await import("./footer/notificationsPanel");
const { useAwayNotice } = await import("./useAwayNotice");

const USER = { id: "alice", name: "alice" };
const SEEN_AT = "2026-11-10T10:00:00.000Z";

const alert = (id: string, seen = false): Alert => ({
  id,
  source: "auto-update",
  severity: "error",
  title: `Alert ${id}`,
  occurrenceCount: 1,
  seen,
  firstOccurrence: "2026-11-10T11:00:00Z",
  lastOccurrence: "2026-11-10T11:00:00Z",
});

const alertList = (unseen: number): AlertList => ({
  alerts: Array.from({ length: unseen + 1 }, (_, index) =>
    alert(`a${index}`, index >= unseen),
  ),
  unseen,
});

const job = (id: string, task: Partial<TaskSnapshot>): TaskHistoryEntry => ({
  task: {
    id,
    type: "storage.btrfs_scrub_start",
    state: "failed",
    created_at: "2026-11-10T08:00:00Z",
    started_at: "2026-11-10T08:01:00Z",
    updated_at: "2026-11-10T08:02:00Z",
    owner: { username: "alice" },
    ...task,
  },
});

const history = (entries: TaskHistoryEntry[]): TaskHistory => ({
  entries,
  more: true,
});

function answer({
  alerts = alertList(0),
  jobs = history([]),
  host,
  failing,
}: {
  alerts?: AlertList;
  failing?: string;
  host?: TaskHostStatus;
  jobs?: TaskHistory;
} = {}) {
  mocks.call.mockImplementation(async (route: string) => {
    if (route === failing) throw new Error("bridge unavailable");
    if (route === "alerts.list") return alerts;
    if (route === "tasks.history") return jobs;
    if (route === "tasks.host" && host) return host;
    throw new Error(`unexpected route ${route}`);
  });
}

const routesCalled = () => mocks.call.mock.calls.map(([route]) => route);

interface SetupOptions {
  historySeenAt?: string;
  privileged: boolean;
  strict?: boolean;
}

function setup({ historySeenAt, privileged, strict = false }: SetupOptions) {
  const queryClient = createTestQueryClient();
  const { bridge } = buildTestConfigSnapshots();
  queryClient.setQueryData(bridgeConfigQueryKey(USER.id), {
    ...bridge,
    ...(historySeenAt ? { dismissals: { historySeenAt } } : {}),
  });
  const auth = createAuthContextValue({
    isAuthenticated: true,
    privileged,
    user: USER,
  });
  const updateConfig = vi.fn();
  const config = createConfigContextValue({ updateConfig });
  const rootRoute = createRootRoute();
  const router = createRouter({
    history: createMemoryHistory({ initialEntries: ["/"] }),
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path: "/history" }),
    ]),
  });

  function Wrapper({ children }: { children: ReactNode }) {
    const tree = (
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={auth}>
          <ConfigContext.Provider value={config}>
            <RouterContextProvider router={router}>
              {children}
            </RouterContextProvider>
          </ConfigContext.Provider>
        </AuthContext.Provider>
      </QueryClientProvider>
    );
    return strict ? <StrictMode>{tree}</StrictMode> : tree;
  }

  const mount = () => renderHook(() => useAwayNotice(), { wrapper: Wrapper });
  return { mount, queryClient, router, updateConfig, ...mount() };
}

// The toast's action, as sonner would be handed it.
function shownAction(toastFn: typeof mocks.toast.info) {
  const options = toastFn.mock.calls[0][1] as {
    action: { label: string; onClick: (event: unknown) => void };
  };
  return options.action;
}

// Let queries settle and effects run, for tests that assert nothing happened.
const settle = () => act(() => new Promise((resolve) => setTimeout(resolve)));

beforeEach(() => {
  mocks.call.mockReset();
  mocks.mutate.mockReset();
  for (const fn of Object.values(mocks.toast)) fn.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.useRealTimers();
  focusManager.setFocused(undefined);
  onlineManager.setOnline(true);
});

describe("useAwayNotice for an admin", () => {
  it("tells them how many notifications are new, with a Show that opens the bell", async () => {
    markFreshSignIn();
    answer({ alerts: alertList(3) });
    const opened = vi.fn();
    const unsubscribe = subscribeNotificationsPanel(opened);

    setup({ privileged: true });

    await waitFor(() => expect(mocks.toast.info).toHaveBeenCalledTimes(1));
    expect(mocks.toast.info).toHaveBeenCalledWith(
      "While you were away: 3 new notifications",
      expect.objectContaining({
        action: expect.objectContaining({ label: "Show" }),
      }),
    );
    expect(mocks.toast.warning).not.toHaveBeenCalled();

    act(() => shownAction(mocks.toast.info).onClick({}));

    expect(opened).toHaveBeenCalledTimes(1);
    // Showing them is looking at them, as opening the bell is.
    expect(mocks.mutate).toHaveBeenCalledWith(
      "alerts.mark_all_seen",
      undefined,
    );
    expect(routesCalled()).toEqual(["alerts.list"]);
    unsubscribe();
  });

  it("says one new notification in the singular", async () => {
    markFreshSignIn();
    answer({ alerts: alertList(1) });

    setup({ privileged: true });

    await waitFor(() =>
      expect(mocks.toast.info).toHaveBeenCalledWith(
        "While you were away: 1 new notification",
        expect.anything(),
      ),
    );
  });

  it("says nothing when no notification is new", async () => {
    markFreshSignIn();
    answer({ alerts: alertList(0) });

    setup({ privileged: true });

    await waitFor(() => expect(hasFreshSignIn()).toBe(false));
    await settle();
    expect(mocks.toast.info).not.toHaveBeenCalled();
  });

  it("takes the mark when the alerts fail to load, and says nothing once they do", async () => {
    markFreshSignIn();
    answer({ alerts: alertList(3), failing: "alerts.list" });

    const { queryClient } = setup({ privileged: true });

    await waitFor(() => expect(hasFreshSignIn()).toBe(false));
    answer({ alerts: alertList(3) });
    await act(() => queryClient.refetchQueries());
    await settle();
    expect(routesCalled()).toEqual(["alerts.list", "alerts.list"]);
    expect(mocks.toast.info).not.toHaveBeenCalled();
  });

  it("says nothing after a page reload, which is no fresh sign-in", async () => {
    answer({ alerts: alertList(3) });

    setup({ privileged: true });

    await settle();
    expect(mocks.toast.info).not.toHaveBeenCalled();
    expect(mocks.call).not.toHaveBeenCalled();
  });

  it("shows the notice once per sign-in, under strict mode and after a remount", async () => {
    markFreshSignIn();
    answer({ alerts: alertList(3) });

    const { mount, unmount } = setup({ privileged: true, strict: true });

    await waitFor(() => expect(mocks.toast.info).toHaveBeenCalledTimes(1));
    await settle();
    unmount();
    mount();
    await settle();
    expect(mocks.toast.info).toHaveBeenCalledTimes(1);
  });
});

describe("useAwayNotice for another user", () => {
  const ended = [
    // Failed after the last look.
    job("failed-after", { finished_at: "2026-11-10T11:00:00Z" }),
    // Interrupted after it, with no finish time: its start counts.
    job("interrupted-after", {
      state: "interrupted",
      started_at: "2026-11-10T10:30:00Z",
    }),
    // Failed before the last look: already seen.
    job("failed-before", { finished_at: "2026-11-10T09:00:00Z" }),
    // Ended after it, but neither failed nor interrupted.
    job("completed-after", {
      state: "completed",
      finished_at: "2026-11-10T11:30:00Z",
    }),
    job("cancelled-after", {
      state: "canceled",
      finished_at: "2026-11-10T11:40:00Z",
    }),
  ];

  it("tells them how many of their jobs did not finish, with a Show that goes to the history", async () => {
    markFreshSignIn();
    answer({ jobs: history(ended) });

    const { router } = setup({ privileged: false, historySeenAt: SEEN_AT });

    await waitFor(() => expect(mocks.toast.warning).toHaveBeenCalledTimes(1));
    expect(mocks.toast.warning).toHaveBeenCalledWith(
      "While you were away: 2 of your jobs did not finish",
      expect.objectContaining({
        action: expect.objectContaining({ label: "Show" }),
      }),
    );
    expect(mocks.toast.info).not.toHaveBeenCalled();

    act(() => shownAction(mocks.toast.warning).onClick({}));

    await waitFor(() =>
      expect(router.state.location.pathname).toBe("/history"),
    );
    // Only the first page, and never the admin's alert routes.
    expect(mocks.call.mock.calls).toEqual([
      ["tasks.history", {}, expect.anything()],
    ]);
    expect(mocks.mutate).not.toHaveBeenCalled();
  });

  it("says one of their jobs in the singular", async () => {
    markFreshSignIn();
    answer({ jobs: history(ended.slice(1)) });

    setup({ privileged: false, historySeenAt: SEEN_AT });

    await waitFor(() =>
      expect(mocks.toast.warning).toHaveBeenCalledWith(
        "While you were away: 1 of your jobs did not finish",
        expect.anything(),
      ),
    );
  });

  it("says nothing when no job of theirs failed since they last looked", async () => {
    markFreshSignIn();
    answer({ jobs: history(ended.slice(2)) });

    const { updateConfig } = setup({
      privileged: false,
      historySeenAt: SEEN_AT,
    });

    await waitFor(() => expect(hasFreshSignIn()).toBe(false));
    await settle();
    expect(mocks.toast.warning).not.toHaveBeenCalled();
    expect(updateConfig).not.toHaveBeenCalled();
  });

  it("takes the mark when the history fails to load, and says nothing once it does", async () => {
    markFreshSignIn();
    answer({ jobs: history(ended), failing: "tasks.history" });

    const { queryClient, updateConfig } = setup({
      privileged: false,
      historySeenAt: SEEN_AT,
    });

    await waitFor(() => expect(hasFreshSignIn()).toBe(false));
    answer({ jobs: history(ended) });
    await act(() => queryClient.refetchQueries());
    await settle();
    expect(routesCalled()).toEqual(["tasks.history", "tasks.history"]);
    expect(mocks.toast.warning).not.toHaveBeenCalled();
    expect(updateConfig).not.toHaveBeenCalled();
  });

  it("asks for the history once, not again on focus or reconnect", async () => {
    markFreshSignIn();
    answer({ jobs: history(ended) });

    setup({ privileged: false, historySeenAt: SEEN_AT });
    await waitFor(() => expect(mocks.toast.warning).toHaveBeenCalledTimes(1));

    act(() => {
      focusManager.setFocused(false);
      focusManager.setFocused(true);
    });
    act(() => {
      onlineManager.setOnline(false);
      onlineManager.setOnline(true);
    });
    await settle();

    expect(routesCalled()).toEqual(["tasks.history"]);
  });

  it("takes the first sign-in with no stored time as the baseline, and says nothing", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-11-11T08:00:00.000Z"));
    markFreshSignIn();
    answer({ jobs: history(ended) });

    const { updateConfig } = setup({ privileged: false });

    await waitFor(() => expect(updateConfig).toHaveBeenCalledTimes(1));
    expect(updateConfig).toHaveBeenCalledWith({
      dismissals: { historySeenAt: "2026-11-11T08:00:00.000Z" },
    });
    await settle();
    expect(mocks.toast.warning).not.toHaveBeenCalled();
  });

  it("takes a stored time it cannot read as no time: a new baseline, and nothing said", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-11-11T08:00:00.000Z"));
    markFreshSignIn();
    answer({ jobs: history(ended) });

    const { updateConfig } = setup({
      privileged: false,
      historySeenAt: "last Tuesday",
    });

    await waitFor(() => expect(updateConfig).toHaveBeenCalledTimes(1));
    expect(updateConfig).toHaveBeenCalledWith({
      dismissals: { historySeenAt: "2026-11-11T08:00:00.000Z" },
    });
    await settle();
    expect(mocks.toast.warning).not.toHaveBeenCalled();
  });

  it("keeps the mark while an update waits, and tells them once the job host is back", async () => {
    markFreshSignIn();
    answer({
      jobs: history([]),
      host: { update_pending: true, running: 1 },
    });

    const { queryClient, updateConfig } = setup({
      privileged: false,
      historySeenAt: SEEN_AT,
    });

    await waitFor(() =>
      expect(routesCalled()).toEqual(["tasks.history", "tasks.host"]),
    );
    await settle();
    expect(hasFreshSignIn()).toBe(true);
    expect(mocks.toast.warning).not.toHaveBeenCalled();

    // The update banner's poll finds the older host gone.
    answer({
      jobs: history(ended),
      host: { update_pending: false, running: 0 },
    });
    await act(() =>
      queryClient.refetchQueries({ queryKey: linuxio.tasks.host.queryKey }),
    );

    await waitFor(() =>
      expect(mocks.toast.warning).toHaveBeenCalledWith(
        "While you were away: 2 of your jobs did not finish",
        expect.anything(),
      ),
    );
    expect(hasFreshSignIn()).toBe(false);
    expect(routesCalled()).toEqual([
      "tasks.history",
      "tasks.host",
      "tasks.host",
      "tasks.history",
    ]);
    expect(updateConfig).not.toHaveBeenCalled();
  });

  it("takes the mark on an empty history when no update waits", async () => {
    markFreshSignIn();
    answer({
      jobs: history([]),
      host: { update_pending: false, running: 0 },
    });

    setup({ privileged: false, historySeenAt: SEEN_AT });

    await waitFor(() => expect(hasFreshSignIn()).toBe(false));
    await settle();
    expect(routesCalled()).toEqual(["tasks.history", "tasks.host"]);
    expect(mocks.toast.warning).not.toHaveBeenCalled();
  });

  it("says nothing after a page reload, which is no fresh sign-in", async () => {
    answer({ jobs: history(ended) });

    setup({ privileged: false, historySeenAt: SEEN_AT });

    await settle();
    expect(mocks.toast.warning).not.toHaveBeenCalled();
    expect(mocks.call).not.toHaveBeenCalled();
  });
});

describe("useAwayNotice without session storage", () => {
  it("shows nothing and does not break the page", async () => {
    markFreshSignIn();
    answer({ alerts: alertList(3) });
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("denied", "SecurityError");
    });

    const { result } = setup({ privileged: true });

    await settle();
    expect(result.current).toBeUndefined();
    expect(mocks.toast.info).not.toHaveBeenCalled();
    expect(mocks.call).not.toHaveBeenCalled();
  });
});
