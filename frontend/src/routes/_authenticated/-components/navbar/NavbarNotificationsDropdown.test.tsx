import { render } from "@testing-library/react";
import type { ButtonHTMLAttributes, ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const state = {
  transfers: [] as Array<{ id: string; progress: number; type: string }>,
};

vi.mock("@/hooks/backgroundJobs/useBackgroundJobState", () => ({
  useBackgroundJobState: () => state,
}));

vi.mock("@/hooks/backgroundJobs/useBackgroundJobActions", () => ({
  useBackgroundJobActions: () => ({
    cancelCompression: vi.fn(),
    cancelCopy: vi.fn(),
    cancelDownload: vi.fn(),
    cancelExtraction: vi.fn(),
    cancelJob: vi.fn(),
    cancelMove: vi.fn(),
    cancelUpload: vi.fn(),
    openIndexerDialog: vi.fn(),
  }),
}));

vi.mock("@/hooks/useToastHistory", () => ({
  useClearToastHistory: () => vi.fn(),
  useToastHistory: () => [],
}));

vi.mock("@/hooks/useDismissibleLayer", () => ({
  useDismissibleLayer: () => ({ current: null }),
}));

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
});
