import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { act, fireEvent, render, screen, waitFor } from "@/test/render";

import type { DockerLogEntry } from "./dockerLogs";
import DockerLogsPage from "./DockerLogsPage";

const mocks = vi.hoisted(() => {
  const streamRef: { current: object | null } = { current: null };
  return {
    closeStream: vi.fn(),
    openChannel: vi.fn(() => ({})),
    openStream: vi.fn(),
    streamOptions: null as Record<string, any> | null,
    streamRef,
  };
});

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      docker: {
        ...actual.linuxio.docker,
        list_containers: {
          queryKey: ["linuxio", "docker", "list_containers"],
          queryFn: () =>
            Promise.resolve([
              { Id: "abc123def456789", Names: ["/web"], State: "running" },
              { Id: "fed654cba321000", Names: ["/db"], State: "running" },
              { Id: "000000000000000", Names: ["/old"], State: "exited" },
            ]),
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

vi.mock("@/components/tabbar", () => ({
  RoutedTabSearch: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
}));

vi.mock("@/components/tables/AppVirtualTable", () => ({
  default: ({
    columns,
    data,
    scrollElementRef,
  }: {
    columns: { id: string }[];
    data: DockerLogEntry[];
    scrollElementRef: { current: HTMLDivElement | null };
  }) => (
    <div
      data-columns={columns.map((column) => column.id).join(",")}
      data-testid="docker-logs-table"
      ref={scrollElementRef}
    >
      {data.map((entry) => (
        <div
          data-name={entry.name}
          data-stderr={entry.stderr ? "true" : "false"}
          data-testid="docker-log-row"
          key={entry.seq}
        >
          {entry.line}
        </div>
      ))}
    </div>
  ),
}));

const record = (name: string, ts: string, line: string, stderr = false) =>
  JSON.stringify({ id: "abc123def456", line, name, stderr, ts });

const frame = (...records: string[]) => `${records.join("\n")}\n`;

const rows = () =>
  screen
    .getAllByTestId("docker-log-row")
    .map((node) => `${node.dataset.name}:${node.textContent}`);

const receive = (text: string) => {
  act(() => {
    mocks.streamOptions?.onText(text);
  });
};

describe("DockerLogsPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.streamRef.current = null;
    mocks.streamOptions = null;
    mocks.openStream.mockImplementation((options: Record<string, any>) => {
      mocks.streamOptions = options;
      options.open();
      mocks.streamRef.current = {};
      return true;
    });
    mocks.closeStream.mockImplementation(() => {
      mocks.streamRef.current = null;
    });
  });

  it("opens the merged stream and renders records oldest first", async () => {
    render(<DockerLogsPage onContainerChange={vi.fn()} />);

    await waitFor(() => expect(mocks.openStream).toHaveBeenCalledTimes(1));
    expect(mocks.openChannel).toHaveBeenCalledWith("docker.logs.follow_all", {
      tail: "5000",
    });

    receive(
      frame(
        record("web", "2026-10-05T10:00:03Z", "web three"),
        record("db", "2026-10-05T10:00:01Z", "db one"),
        record("db", "2026-10-05T10:00:02Z", "db two", true),
      ),
    );

    expect(rows()).toEqual(["db:db one", "db:db two", "web:web three"]);
    expect(screen.getAllByTestId("docker-log-row")[1].dataset.stderr).toBe(
      "true",
    );
    expect(screen.getByTestId("docker-logs-table").dataset.columns).toBe(
      "time,container,level,line",
    );
    expect(screen.getByText("3 shown")).toBeInTheDocument();
  });

  it("filters by container and by search text", async () => {
    const { rerender } = render(<DockerLogsPage onContainerChange={vi.fn()} />);
    await waitFor(() => expect(mocks.openStream).toHaveBeenCalledTimes(1));
    receive(
      frame(
        record("web", "2026-10-05T10:00:01Z", "GET /index"),
        record("db", "2026-10-05T10:00:02Z", "ready"),
        record("web", "2026-10-05T10:00:03Z", "GET /about"),
      ),
    );

    rerender(<DockerLogsPage container="web" onContainerChange={vi.fn()} />);
    expect(rows()).toEqual(["web:GET /index", "web:GET /about"]);
    expect(screen.getByTestId("docker-logs-table").dataset.columns).toBe(
      "time,level,line",
    );

    fireEvent.change(screen.getByPlaceholderText("Search logs…"), {
      target: { value: "about" },
    });
    expect(rows()).toEqual(["web:GET /about"]);
  });

  it("lists running containers in the filter and reports the selection", async () => {
    const onContainerChange = vi.fn();
    const { user } = render(
      <DockerLogsPage onContainerChange={onContainerChange} />,
    );
    await waitFor(() => expect(mocks.openStream).toHaveBeenCalledTimes(1));

    await user.click(screen.getByRole("combobox", { name: "Container" }));
    await waitFor(() =>
      expect(screen.getAllByRole("option").map((o) => o.textContent)).toEqual([
        "All containers",
        "db",
        "web",
      ]),
    );
    await user.click(screen.getByRole("option", { name: "db" }));
    expect(onContainerChange).toHaveBeenCalledWith("db");
  });

  it("reopens with tail 0 when live resumes", async () => {
    render(<DockerLogsPage onContainerChange={vi.fn()} />);
    await waitFor(() => expect(mocks.openStream).toHaveBeenCalledTimes(1));
    receive(frame(record("web", "2026-10-05T10:00:01Z", "first")));
    const live = screen.getByRole("checkbox", { name: "Live" });
    fireEvent.click(live);
    expect(mocks.closeStream).toHaveBeenCalledTimes(1);
    expect(rows()).toEqual(["web:first"]);

    fireEvent.click(live);
    await waitFor(() => expect(mocks.openStream).toHaveBeenCalledTimes(2));
    expect(mocks.openChannel).toHaveBeenLastCalledWith(
      "docker.logs.follow_all",
      { tail: "0" },
    );
  });
});
