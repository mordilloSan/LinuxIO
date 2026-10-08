import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ButtonHTMLAttributes, ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { Alert } from "@/api";

const state = {
  transfers: [] as Array<{ id: string; progress: number; type: string }>,
};

vi.mock("@/hooks/backgroundTasks/useBackgroundTaskState", () => ({
  useBackgroundTaskList: () => state.transfers,
  useBackgroundTasks: () => state.transfers,
  useBackgroundTask: (id: string) =>
    state.transfers.find((item) => item.id === id),
}));

vi.mock("@/hooks/backgroundTasks/useBackgroundTaskActions", () => ({
  useBackgroundTaskActions: () => ({
    cancelCompression: vi.fn(),
    cancelCopy: vi.fn(),
    cancelDownload: vi.fn(),
    cancelExtraction: vi.fn(),
    cancelTask: vi.fn(),
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

vi.mock("@/components/ui/AppRouterLinkButton", () => ({
  default: () => <button />,
}));

vi.mock("@/components/ui/AppTooltip", () => ({
  default: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

const { NavbarNotificationsDropdown } =
  await import("./NavbarNotificationsDropdown");

describe("NavbarNotificationsDropdown peek timer", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    state.transfers = [];
    useAlertsMock.mockReturnValue(noAlerts());
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("clears the outstanding hide timer when unmounted", () => {
    const view = render(<NavbarNotificationsDropdown />);

    state.transfers = [{ id: "transfer-1", progress: 10, type: "download" }];
    view.rerender(<NavbarNotificationsDropdown />);

    expect(vi.getTimerCount()).toBe(2);

    state.transfers = [];
    view.rerender(<NavbarNotificationsDropdown />);
    expect(vi.getTimerCount()).toBe(1);

    view.unmount();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("portals the notifications panel outside the footer row", () => {
    const view = render(<NavbarNotificationsDropdown />);

    fireEvent.click(screen.getByRole("button", { name: "Notifications" }));

    const panel = screen.getByRole("dialog", { name: "Notifications" });
    expect(view.container).not.toContainElement(panel);
    expect(panel.closest(".app-popover-root")).toBeInTheDocument();
  });
});

describe("NavbarNotificationsDropdown alerts", () => {
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
    render(<NavbarNotificationsDropdown />);
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
    render(<NavbarNotificationsDropdown />);
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
    render(<NavbarNotificationsDropdown />);
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
    render(<NavbarNotificationsDropdown />);
    await userEvent.click(
      screen.getByRole("button", { name: "Notifications" }),
    );
    await screen.findByText("Automatic updates installed 3 packages");
    expect(markAllSeen).not.toHaveBeenCalled();
  });
});
