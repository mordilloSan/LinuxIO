import { describe, expect, it, vi } from "vitest";

const logStore = vi.hoisted(() => ({
  listeners: new Set<() => void>(),
  logs: "initial log",
  version: 0,
}));
const frostedCardRender = vi.hoisted(() => vi.fn());

vi.mock("@/components/cards/FrostedCard", () => ({
  default: ({ children }: { children: React.ReactNode }) => {
    frostedCardRender();
    return <section>{children}</section>;
  },
}));

vi.mock("@/hooks/useLogStream", async () => {
  const { useRef, useSyncExternalStore } =
    await vi.importActual<typeof import("react")>("react");

  return {
    useLogStream: () => {
      useSyncExternalStore(
        (listener) => {
          logStore.listeners.add(listener);
          return () => logStore.listeners.delete(listener);
        },
        () => logStore.version,
        () => logStore.version,
      );
      return {
        error: null,
        isLoading: false,
        liveMode: true,
        logs: logStore.logs,
        logsBoxRef: useRef<HTMLDivElement>(null),
        setLiveMode: vi.fn(),
      };
    },
  };
});

const UnitLogsCard = (await import("@/components/cards/UnitLogsCard")).default;
const { act, render, screen } = await import("@/test/render");

describe("UnitLogsCard", () => {
  it("keeps the FrostedCard shell out of live log updates", () => {
    render(<UnitLogsCard title="Service Logs" unitName="demo.service" />);

    expect(screen.getByText("initial log")).toBeInTheDocument();
    expect(frostedCardRender).toHaveBeenCalledTimes(1);

    act(() => {
      logStore.logs = "next log";
      logStore.version += 1;
      logStore.listeners.forEach((listener) => listener());
    });

    expect(screen.getByText("next log")).toBeInTheDocument();
    expect(frostedCardRender).toHaveBeenCalledTimes(1);
  });
});
