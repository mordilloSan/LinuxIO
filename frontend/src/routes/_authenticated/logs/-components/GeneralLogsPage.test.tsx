import type { QueryClient } from "@tanstack/react-query";
import type { UIEventHandler } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  act,
  createTestQueryClient,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@/test/render";

import GeneralLogsPage from "./GeneralLogsPage";

const mocks = vi.hoisted(() => {
  const streamRef: { current: object | null } = { current: null };
  return {
    closeStream: vi.fn(),
    fetchPage: vi.fn(),
    navigate: vi.fn(),
    openChannel: vi.fn(() => ({})),
    openStream: vi.fn(),
    scrollMetrics: { clientHeight: 500, scrollHeight: 1000 },
    streamOptions: null as Record<string, any> | null,
    streamRef,
  };
});

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => mocks.navigate,
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      logs: {
        ...actual.linuxio.logs,
        general_entry: (request: { cursor: string }) => ({
          queryKey: ["linuxio", "logs", "general_entry", request],
          queryFn: () => Promise.resolve(undefined),
        }),
        general_page: (request: unknown) => ({
          queryKey: ["linuxio", "logs", "general_page", request],
          queryFn: () => mocks.fetchPage(request),
        }),
      },
      systemd: {
        ...actual.linuxio.systemd,
        list_services: {
          queryKey: ["linuxio", "systemd", "list_services"],
          queryFn: () => Promise.resolve([]),
        },
      },
    },
    openChannel: mocks.openChannel,
    useStreamMux: () => ({ isOpen: true }),
  };
});

vi.mock("@/hooks/useLiveStream", () => ({
  useLiveStream: () => ({
    closeStream: mocks.closeStream,
    openStream: mocks.openStream,
    streamRef: mocks.streamRef,
  }),
}));

vi.mock("@/components/tables/AppDataTable", () => ({
  default: ({
    data,
    onScroll,
    scrollElementRef,
  }: {
    data: Array<{ id: string }>;
    onScroll?: UIEventHandler<HTMLDivElement>;
    scrollElementRef: { current: HTMLDivElement | null };
  }) => (
    <div
      data-row-ids={data.map((entry) => entry.id).join(",")}
      data-testid="logs-scroll"
      onScroll={onScroll}
      ref={(node) => {
        if (!node) return;
        scrollElementRef.current = node;
        Object.defineProperties(node, {
          clientHeight: {
            configurable: true,
            get: () => mocks.scrollMetrics.clientHeight,
          },
          scrollHeight: {
            configurable: true,
            get: () => mocks.scrollMetrics.scrollHeight,
          },
        });
      }}
    >
      {data.length}
    </div>
  ),
}));

const journalEntry = (cursor: string, timestamp: number) =>
  JSON.stringify({
    __CURSOR: cursor,
    __REALTIME_TIMESTAMP: String(timestamp),
    MESSAGE: cursor,
    PRIORITY: "6",
    SYSLOG_IDENTIFIER: "test",
  });

describe("GeneralLogsPage cursor pagination", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    queryClient = createTestQueryClient();
    vi.spyOn(queryClient, "query");
    mocks.streamOptions = null;
    mocks.streamRef.current = null;
    mocks.scrollMetrics.clientHeight = 500;
    mocks.scrollMetrics.scrollHeight = 1000;
    mocks.fetchPage.mockReset();
    mocks.openChannel.mockClear();
    mocks.openStream.mockImplementation((options) => {
      mocks.streamOptions = options;
      options.open();
      mocks.streamRef.current = {};
      return true;
    });
    mocks.closeStream.mockImplementation(() => {
      mocks.streamRef.current = null;
    });

    let nextFrame = 0;
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      const id = ++nextFrame;
      queueMicrotask(() => callback(0));
      return id;
    });
    vi.stubGlobal("cancelAnimationFrame", vi.fn());
  });

  it("prefetches a strictly older page near the bottom and appends it", async () => {
    mocks.scrollMetrics.scrollHeight = 2000;
    mocks.fetchPage.mockResolvedValue({
      entries: [journalEntry("older-1", 1_000_000), journalEntry("older-2", 0)],
      hasMore: false,
    });
    render(<GeneralLogsPage />, { queryClient });

    await waitFor(() => expect(mocks.streamOptions).not.toBeNull());
    act(() => {
      mocks.streamOptions?.onText(
        `${journalEntry("old", 2_000_000)}\n${journalEntry("new", 3_000_000)}\n`,
      );
      mocks.streamOptions?.onProgress({
        type: "backlog_complete",
        count: 500,
        truncated: false,
      });
    });

    const scrollElement = await screen.findByTestId("logs-scroll");
    await waitFor(() =>
      expect(scrollElement).toHaveAttribute("data-row-ids", "new,old"),
    );

    Object.defineProperty(scrollElement, "scrollTop", {
      configurable: true,
      // 1100 px from the bottom: prefetch before the user hits the end.
      value: 400,
      writable: true,
    });
    fireEvent.scroll(scrollElement);

    await waitFor(() =>
      expect(mocks.fetchPage).toHaveBeenCalledWith(
        expect.objectContaining({
          cursor: "old",
          lines: "1000",
          timePeriod: "24h",
        }),
      ),
    );
    await waitFor(() =>
      expect(scrollElement).toHaveAttribute(
        "data-row-ids",
        "new,old,older-1,older-2",
      ),
    );
  });

  it("resumes live mode strictly after the newest buffered cursor", async () => {
    render(<GeneralLogsPage />, { queryClient });
    await waitFor(() => expect(mocks.streamOptions).not.toBeNull());
    expect(mocks.openChannel.mock.calls[0]).toEqual([
      "logs.general.follow",
      {
        afterCursor: "",
        fieldFilters: [],
        follow: true,
        identifier: "",
        lines: "500",
        priority: "",
        timePeriod: "24h",
      },
    ]);
    expect(screen.queryByText("Lines")).not.toBeInTheDocument();

    act(() => {
      mocks.streamOptions?.onText(
        `${journalEntry("old", 2_000_000)}\n${journalEntry("new", 3_000_000)}\n`,
      );
      mocks.streamOptions?.onProgress({
        type: "backlog_complete",
        count: 2,
        truncated: false,
      });
    });
    await screen.findByTestId("logs-scroll");

    const liveSwitch = screen.getByRole("checkbox");
    fireEvent.click(liveSwitch);
    fireEvent.click(liveSwitch);

    await waitFor(() => expect(mocks.openChannel).toHaveBeenCalledTimes(2));
    expect(mocks.openChannel).toHaveBeenLastCalledWith("logs.general.follow", {
      afterCursor: "new",
      fieldFilters: [],
      follow: true,
      identifier: "",
      lines: "0",
      priority: "",
      timePeriod: "24h",
    });
  });

  it("keeps pagination available when progress arrives before buffered rows flush", async () => {
    mocks.scrollMetrics.clientHeight = 0;
    mocks.scrollMetrics.scrollHeight = 0;
    mocks.fetchPage.mockResolvedValue({ entries: [], hasMore: false });
    render(<GeneralLogsPage />, { queryClient });
    await waitFor(() => expect(mocks.streamOptions).not.toBeNull());

    act(() => {
      mocks.streamOptions?.onProgress({
        type: "backlog_complete",
        count: 500,
        truncated: false,
      });
    });
    await screen.findByTestId("logs-scroll");
    await act(async () => {
      await Promise.resolve();
    });
    expect(mocks.fetchPage).not.toHaveBeenCalled();

    act(() => {
      mocks.streamOptions?.onText(
        `${journalEntry("old", 2_000_000)}\n${journalEntry("new", 3_000_000)}\n`,
      );
    });

    const scrollElement = screen.getByTestId("logs-scroll");
    await waitFor(() =>
      expect(scrollElement).toHaveAttribute("data-row-ids", "new,old"),
    );
    expect(mocks.fetchPage).not.toHaveBeenCalled();
    fireEvent.scroll(scrollElement);

    await waitFor(() =>
      expect(mocks.fetchPage).toHaveBeenCalledWith(
        expect.objectContaining({ cursor: "old" }),
      ),
    );
    expect(queryClient.query).toHaveBeenCalledWith(
      expect.objectContaining({ gcTime: 0, staleTime: 0 }),
    );
  });
});
