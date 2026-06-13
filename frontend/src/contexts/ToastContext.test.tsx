import type React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const sonnerMocks = vi.hoisted(() => ({
  dismiss: vi.fn(),
  getHistory: vi.fn(),
  toasts: [] as Array<{
    description?: string;
    id: string | number;
    meta?: { href?: string; label?: string };
    title?: React.ReactNode;
    type?: string;
  }>,
}));

vi.mock("sonner", () => ({
  Toaster: () => <div data-testid="toaster" />,
  toast: {
    dismiss: sonnerMocks.dismiss,
    getHistory: sonnerMocks.getHistory,
  },
  useSonner: () => ({
    toasts: sonnerMocks.toasts,
  }),
}));

const { ToastProvider } = await import("@/contexts/ToastContext");
const { useClearToastHistory, useToastHistory } =
  await import("@/hooks/useToastHistory");
const { act, render, screen } = await import("@/test/render");

function Probe({ limit = 5 }: { limit?: number }) {
  const history = useToastHistory(limit);
  const clearHistory = useClearToastHistory();
  return (
    <div>
      <div data-testid="history">
        {history
          .map((item) =>
            [
              item.title,
              item.description ?? "",
              item.type ?? "",
              item.meta?.href ?? "",
            ].join(":"),
          )
          .join("|")}
      </div>
      <button onClick={clearHistory}>clear</button>
    </div>
  );
}

function renderProvider(limit?: number) {
  return render(
    <ToastProvider>
      <Probe limit={limit} />
    </ToastProvider>,
  );
}

describe("ToastProvider", () => {
  beforeEach(() => {
    sonnerMocks.toasts = [];
    sonnerMocks.getHistory.mockReturnValue([]);
  });

  it("records active sonner toasts into history and localStorage", () => {
    vi.setSystemTime(new Date("2026-01-01T00:00:00Z"));
    sonnerMocks.toasts = [
      {
        description: "Compose stack is up",
        id: 1,
        meta: { href: "/docker", label: "Open Docker" },
        title: "Started",
        type: "success",
      },
    ];

    renderProvider();

    expect(screen.getByTestId("history")).toHaveTextContent(
      "Started:Compose stack is up:success:/docker",
    );
    expect(localStorage.getItem("linuxio.toastHistory")).toContain("Started");
  });

  it("loads stored history and applies hook limits", () => {
    localStorage.setItem(
      "linuxio.toastHistory",
      JSON.stringify([
        {
          createdAt: 20,
          id: "stored-2",
          title: "newer",
          type: "info",
        },
        {
          createdAt: 10,
          id: "stored-1",
          title: "older",
          type: "warning",
        },
      ]),
    );

    renderProvider(1);

    expect(screen.getByTestId("history")).toHaveTextContent("newer::info:");
    expect(screen.getByTestId("history")).not.toHaveTextContent("older");
  });

  it("coerces invalid stored titles and ignores malformed storage", () => {
    localStorage.setItem(
      "linuxio.toastHistory",
      JSON.stringify([
        {
          createdAt: 1,
          id: "bad-title",
          title: { not: "text" },
        },
      ]),
    );

    renderProvider();

    expect(screen.getByTestId("history")).toHaveTextContent("Notification");
  });

  it("clears persisted history, dismisses active toasts, and ignores current active toast ids", async () => {
    vi.setSystemTime(new Date("2026-01-01T00:00:00Z"));
    const activeToast = {
      id: "toast-1",
      title: "Still visible",
      type: "info",
    };
    sonnerMocks.toasts = [activeToast];
    sonnerMocks.getHistory.mockReturnValue([activeToast]);
    const { rerender } = renderProvider();

    expect(screen.getByTestId("history")).toHaveTextContent("Still visible");

    await act(async () => {
      screen.getByRole("button", { name: "clear" }).click();
    });
    rerender(
      <ToastProvider>
        <Probe />
      </ToastProvider>,
    );

    expect(sonnerMocks.dismiss).toHaveBeenCalledTimes(1);
    expect(localStorage.getItem("linuxio.toastHistory")).toBe("[]");
    expect(screen.getByTestId("history")).toHaveTextContent("");
  });
});
