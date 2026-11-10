import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterContextProvider,
} from "@tanstack/react-router";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ButtonHTMLAttributes, ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { Alert } from "@/api";
import type { BackgroundTaskLink } from "@/tasks/types";

const state = {
  transfers: [] as Array<{
    cancelRequested?: boolean;
    id: string;
    indeterminate?: boolean;
    label?: string;
    link?: BackgroundTaskLink;
    progress: number;
    type: string;
  }>,
};
const cancelTask = vi.hoisted(() => vi.fn());

vi.mock("@/tasks/useBackgroundTaskState", () => ({
  useBackgroundTaskList: () => state.transfers,
  useBackgroundTasks: () => state.transfers,
  useBackgroundTask: (id: string) =>
    state.transfers.find((item) => item.id === id),
}));

vi.mock("@/tasks/useBackgroundTaskActions", () => ({
  useBackgroundTaskActions: () => ({
    cancelCompression: vi.fn(),
    cancelCopy: vi.fn(),
    cancelDownload: vi.fn(),
    cancelExtraction: vi.fn(),
    cancelTask,
    cancelMove: vi.fn(),
    cancelUpload: vi.fn(),
    openIndexerDialog: vi.fn(),
  }),
}));

const useAlertsMock = vi.fn();
vi.mock("./useAlerts", () => ({ useAlerts: () => useAlertsMock() }));

const alertFixture = (over: Partial<Alert> = {}): Alert => ({
  id: "auto-update/run",
  source: "auto-update",
  severity: "info",
  title: "Automatic updates installed 3 packages",
  message: "curl libssl3 openssl",
  link: "/updates",
  occurrenceCount: 1,
  seen: false,
  firstOccurrence: "2026-10-08T06:00:00Z",
  lastOccurrence: "2026-10-08T06:00:00Z",
  ...over,
});

const noAlerts = () => ({
  alerts: [] as Alert[],
  unseen: 0,
  enabled: true,
  markAllSeen: vi.fn(),
  dismiss: vi.fn(),
});

vi.mock("@/theme", () => ({
  useAppTheme: () => ({
    palette: {
      error: { main: "red" },
      info: { main: "blue" },
      success: { main: "green" },
      text: { secondary: "gray" },
      warning: { main: "yellow" },
    },
  }),
}));

vi.mock("@/theme/constants", () => ({ iconSize: { md: 20 } }));

vi.mock("@iconify/react", () => ({
  Icon: () => null,
}));

vi.mock("@/components/ui/AppButton", () => ({
  default: ({
    children,
    ...props
  }: ButtonHTMLAttributes<HTMLButtonElement>) => (
    <button {...props}>{children}</button>
  ),
}));

vi.mock("@/components/ui/AppIconButton", () => ({
  default: ({
    children,
    ...props
  }: ButtonHTMLAttributes<HTMLButtonElement>) => (
    <button {...props}>{children}</button>
  ),
}));

vi.mock("@/components/ui/AppLinearProgress", () => ({
  default: () => <div />,
}));

vi.mock("@/components/ui/AppTooltip", () => ({
  default: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

const { NotificationsBell } = await import("./NotificationsBell");

// Alert and scheduled-run rows link to their pages through the real router
// link, which needs a router holding those pages.
function renderBell() {
  const rootRoute = createRootRoute();
  const router = createRouter({
    history: createMemoryHistory(),
    routeTree: rootRoute.addChildren([
      createRoute({
        getParentRoute: () => rootRoute,
        path: "/services/schedules",
      }),
      createRoute({ getParentRoute: () => rootRoute, path: "/updates" }),
      createRoute({ getParentRoute: () => rootRoute, path: "/history" }),
    ]),
  });
  return render(<NotificationsBell />, {
    wrapper: ({ children }: { children: ReactNode }) => (
      <RouterContextProvider router={router}>{children}</RouterContextProvider>
    ),
  });
}

describe("NotificationsBell peek timer", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    state.transfers = [];
    useAlertsMock.mockReturnValue(noAlerts());
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("clears the outstanding hide timer when unmounted", () => {
    const view = render(<NotificationsBell />);

    state.transfers = [{ id: "transfer-1", progress: 10, type: "download" }];
    view.rerender(<NotificationsBell />);

    expect(vi.getTimerCount()).toBe(2);

    state.transfers = [];
    view.rerender(<NotificationsBell />);
    expect(vi.getTimerCount()).toBe(1);

    view.unmount();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("portals the notifications panel outside the footer row", () => {
    const view = renderBell();

    fireEvent.click(screen.getByRole("button", { name: "Notifications" }));

    const panel = screen.getByRole("dialog", { name: "Notifications" });
    expect(view.container).not.toContainElement(panel);
    expect(panel.closest(".app-popover-root")).toBeInTheDocument();
  });
});

describe("NotificationsBell running jobs", () => {
  beforeEach(() => {
    cancelTask.mockClear();
    useAlertsMock.mockReturnValue(noAlerts());
  });

  it("cancels a running job from its row", async () => {
    state.transfers = [
      { id: "job-1", label: "Balancing", progress: 40, type: "task" },
    ];
    renderBell();
    await userEvent.click(
      screen.getByRole("button", { name: "Notifications" }),
    );
    expect(await screen.findByText("40%")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Cancel task" }));
    expect(cancelTask).toHaveBeenCalledWith("job-1");
  });

  // A canceled job keeps running until its handler returns.
  it("shows a job whose cancel was requested as cancelling, with no second cancel", async () => {
    state.transfers = [
      {
        cancelRequested: true,
        id: "job-1",
        label: "Balancing",
        progress: 40,
        type: "task",
      },
    ];
    renderBell();
    await userEvent.click(
      screen.getByRole("button", { name: "Notifications" }),
    );
    expect(await screen.findByText("Cancelling…")).toBeInTheDocument();
    expect(screen.queryByText("40%")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Cancel task" })).toBeDisabled();
  });
});

// A systemd timer runs these: the bridge refuses to cancel one, so the row
// offers a link to the page that owns the run where others offer a cancel.
describe("NotificationsBell scheduled runs", () => {
  const scheduledScript = {
    id: "run-1",
    indeterminate: true,
    label: "Nightly backup",
    link: { search: { schedule: "sched-1" }, to: "/services/schedules" },
    progress: 0,
    type: "task",
  };

  const openTray = async () => {
    await userEvent.click(
      screen.getByRole("button", { name: "Notifications" }),
    );
    return screen.findByRole("link", { name: "Open Nightly backup" });
  };

  beforeEach(() => {
    cancelTask.mockClear();
    useAlertsMock.mockReturnValue(noAlerts());
  });

  it("links the row to the run's page instead of offering a cancel", async () => {
    state.transfers = [scheduledScript];
    renderBell();

    const link = await openTray();
    expect(link).toHaveTextContent("Open");
    expect(link).toHaveAttribute(
      "href",
      "/services/schedules?schedule=sched-1",
    );
    expect(
      screen.queryByRole("button", { name: "Cancel task" }),
    ).not.toBeInTheDocument();
    // The row stays an ordinary list item; only the link is interactive.
    expect(link.closest("li")).not.toHaveAttribute("role");
    expect(link.closest("li")).not.toHaveAttribute("tabindex");
  });

  it("reaches the link from the keyboard", async () => {
    state.transfers = [scheduledScript];
    renderBell();

    const link = await openTray();
    await userEvent.tab();
    expect(link).toHaveFocus();
  });

  it("closes the tray when the link is followed", async () => {
    state.transfers = [scheduledScript];
    renderBell();

    await userEvent.click(await openTray());
    expect(
      screen.queryByRole("dialog", { name: "Notifications" }),
    ).not.toBeInTheDocument();
  });

  it("shows a run whose stop was requested as cancelling", async () => {
    state.transfers = [{ ...scheduledScript, cancelRequested: true }];
    renderBell();

    const link = await openTray();
    expect(screen.getByText("Cancelling…")).toBeInTheDocument();
    expect(link).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Cancel task" }),
    ).not.toBeInTheDocument();
  });
});

describe("NotificationsBell alerts", () => {
  beforeEach(() => {
    state.transfers = [];
    useAlertsMock.mockReturnValue(noAlerts());
  });

  it("renders alerts and marks them seen when opened", async () => {
    const markAllSeen = vi.fn();
    useAlertsMock.mockReturnValue({
      ...noAlerts(),
      alerts: [alertFixture()],
      unseen: 1,
      markAllSeen,
    });
    renderBell();
    await userEvent.click(
      screen.getByRole("button", { name: "Notifications" }),
    );
    expect(
      await screen.findByText("Automatic updates installed 3 packages"),
    ).toBeInTheDocument();
    expect(markAllSeen).toHaveBeenCalledTimes(1);
  });

  it("shows the unseen count on the trigger", () => {
    useAlertsMock.mockReturnValue({
      ...noAlerts(),
      alerts: [
        alertFixture(),
        alertFixture({
          id: "docker-update/x",
          severity: "error",
          title: "Docker update failed: x",
        }),
      ],
      unseen: 2,
    });
    renderBell();
    expect(
      screen.getByRole("button", { name: "Notifications" }),
    ).toHaveTextContent("2");
  });

  it("dismisses an alert", async () => {
    const dismiss = vi.fn();
    useAlertsMock.mockReturnValue({
      ...noAlerts(),
      alerts: [alertFixture()],
      dismiss,
    });
    renderBell();
    await userEvent.click(
      screen.getByRole("button", { name: "Notifications" }),
    );
    await userEvent.click(
      await screen.findByRole("button", { name: "Dismiss alert" }),
    );
    expect(dismiss).toHaveBeenCalledWith("auto-update/run");
  });

  it("does not call markAllSeen when nothing is unseen", async () => {
    const markAllSeen = vi.fn();
    useAlertsMock.mockReturnValue({
      ...noAlerts(),
      alerts: [alertFixture({ seen: true })],
      markAllSeen,
    });
    renderBell();
    await userEvent.click(
      screen.getByRole("button", { name: "Notifications" }),
    );
    await screen.findByText("Automatic updates installed 3 packages");
    expect(markAllSeen).not.toHaveBeenCalled();
  });
});

describe("NotificationsBell history link", () => {
  const openTray = () =>
    userEvent.click(screen.getByRole("button", { name: "Notifications" }));

  beforeEach(() => {
    state.transfers = [];
    useAlertsMock.mockReturnValue(noAlerts());
  });

  it("links the empty tray to the history page", async () => {
    renderBell();

    await openTray();

    expect(await screen.findByText("No notifications")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "History" })).toHaveAttribute(
      "href",
      "/history",
    );
  });

  it("keeps the link below a list of notifications", async () => {
    useAlertsMock.mockReturnValue({ ...noAlerts(), alerts: [alertFixture()] });
    renderBell();

    await openTray();

    const link = await screen.findByRole("link", { name: "History" });
    const item = screen.getByText("Automatic updates installed 3 packages");
    expect(
      item.compareDocumentPosition(link) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("closes the tray when the link is followed", async () => {
    renderBell();

    await openTray();
    await userEvent.click(await screen.findByRole("link", { name: "History" }));

    expect(
      screen.queryByRole("dialog", { name: "Notifications" }),
    ).not.toBeInTheDocument();
  });
});
