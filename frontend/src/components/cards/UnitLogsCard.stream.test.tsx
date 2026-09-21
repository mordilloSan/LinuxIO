import { expect, it, vi } from "vitest";

import { openChannel, type Stream } from "@/api";
import UnitLogsCard from "@/components/cards/UnitLogsCard";
import { act, render, screen } from "@/test/render";

vi.mock("@/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api")>()),
  openChannel: vi.fn(),
  useStreamMux: () => ({ isOpen: true }),
}));

it.each([false, true])(
  "replaces the selected unit's logs while preserving paused=%s",
  (paused) => {
    vi.mocked(openChannel).mockReset();
    vi.useFakeTimers({
      toFake: [
        "setTimeout",
        "clearTimeout",
        "requestAnimationFrame",
        "cancelAnimationFrame",
      ],
    });
    const streams = [1, 2].map((id): Stream => ({
      abort: vi.fn(),
      close: vi.fn(),
      id,
      onClose: null,
      onData: null,
      onProgress: null,
      onResult: null,
      resize: vi.fn(),
      status: "open",
      type: "request",
      write: vi.fn(),
    }));
    vi.mocked(openChannel)
      .mockReturnValueOnce(streams[0])
      .mockReturnValueOnce(streams[1]);
    const { rerender, unmount } = render(
      <UnitLogsCard title="Logs" unitName="one.service" />,
    );

    act(() => {
      streams[0].onData?.(new TextEncoder().encode("first unit output"));
      vi.advanceTimersByTime(20);
    });
    expect(screen.getByText("first unit output")).toBeInTheDocument();

    rerender(<UnitLogsCard title="Logs" unitName="one.service" />);
    expect(openChannel).toHaveBeenCalledTimes(1);
    if (paused) {
      act(() => {
        screen.getByRole("checkbox", { name: "Live" }).click();
      });
      expect(screen.getByRole("checkbox", { name: "Live" })).not.toBeChecked();
    }
    rerender(<UnitLogsCard title="Logs" unitName="two.service" />);

    expect(streams[0].close).toHaveBeenCalledTimes(1);
    expect(screen.queryByText("first unit output")).not.toBeInTheDocument();
    if (paused) {
      expect(screen.getByRole("checkbox", { name: "Live" })).not.toBeChecked();
      expect(openChannel).toHaveBeenCalledTimes(1);
      expect(screen.getByText("No logs available.")).toBeInTheDocument();
      act(() => {
        screen.getByRole("checkbox", { name: "Live" }).click();
      });
    }
    expect(screen.getByRole("checkbox", { name: "Live" })).toBeChecked();
    expect(openChannel).toHaveBeenLastCalledWith("logs.service.follow", {
      serviceName: "two.service",
      lines: paused ? "0" : "200",
    });
    act(() => {
      streams[1].onData?.(new TextEncoder().encode("second unit output"));
      vi.advanceTimersByTime(20);
    });
    expect(screen.getByText("second unit output")).toBeInTheDocument();
    unmount();
    expect(streams[1].close).toHaveBeenCalledTimes(1);
  },
);
