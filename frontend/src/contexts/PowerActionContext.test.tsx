import { beforeEach, describe, expect, it, vi } from "vitest";

import { PowerActionProvider } from "@/contexts/PowerActionProvider";
import usePowerAction from "@/hooks/usePowerAction";
import { act, render, screen } from "@/test/render";

function Probe() {
  const { triggerPowerOff, triggerReboot } = usePowerAction();
  return (
    <div>
      <button onClick={triggerReboot}>reboot</button>
      <button onClick={triggerPowerOff}>power off</button>
    </div>
  );
}

function renderProvider() {
  return render(
    <PowerActionProvider>
      <Probe />
    </PowerActionProvider>,
  );
}

describe("PowerActionProvider", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
  });

  it("shows the shutdown overlay without starting reboot polling", async () => {
    vi.useFakeTimers();
    renderProvider();

    await act(async () => {
      screen.getByRole("button", { name: "power off" }).click();
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000);
    });

    expect(screen.getByText("Shutting Down...")).toBeInTheDocument();
    expect(
      screen.getByText(
        "The system is shutting down. You may close this window.",
      ),
    ).toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });

  it("shows the reboot overlay and polls until the server responds", async () => {
    vi.useFakeTimers();
    vi.mocked(fetch).mockResolvedValue({ ok: false } as Response);
    renderProvider();

    await act(async () => {
      screen.getByRole("button", { name: "reboot" }).click();
    });

    expect(screen.getByText("Rebooting...")).toBeInTheDocument();
    expect(
      screen.getByText(
        "Please wait while the system restarts. You will be redirected once the server is back online.",
      ),
    ).toBeInTheDocument();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });

    expect(fetch).toHaveBeenCalledWith("/api/version", {
      cache: "no-store",
      method: "GET",
      signal: expect.any(AbortSignal),
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000);
    });

    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it("keeps retrying reboot polling after transient fetch failures", async () => {
    vi.useFakeTimers();
    vi.mocked(fetch).mockRejectedValue(new Error("offline"));
    renderProvider();

    await act(async () => {
      screen.getByRole("button", { name: "reboot" }).click();
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(8000);
    });

    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it("times out stalled attempts and aborts the pending request on unmount", async () => {
    vi.useFakeTimers();
    const signals: AbortSignal[] = [];
    vi.mocked(fetch).mockImplementation(
      (_url, options) =>
        new Promise((_resolve, reject) => {
          const signal = options?.signal;
          if (!signal) return;
          signals.push(signal);
          signal.addEventListener("abort", () => reject(signal.reason), {
            once: true,
          });
        }),
    );
    const { unmount } = renderProvider();
    await act(async () => {
      screen.getByRole("button", { name: "reboot" }).click();
    });
    await act(async () => vi.advanceTimersByTimeAsync(5000));
    expect(signals).toHaveLength(1);
    expect(signals[0].aborted).toBe(false);
    await act(async () => vi.advanceTimersByTimeAsync(10_000));
    expect(signals[0].aborted).toBe(true);
    await act(async () => vi.advanceTimersByTimeAsync(3000));
    expect(signals).toHaveLength(2);
    expect(signals[1].aborted).toBe(false);
    unmount();
    expect(signals[1].aborted).toBe(true);
    await act(async () => vi.advanceTimersByTimeAsync(30_000));
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it("stops scheduled reboot retries when the provider unmounts", async () => {
    vi.useFakeTimers();
    vi.mocked(fetch).mockResolvedValue({ ok: false } as Response);
    const rendered = renderProvider();

    await act(async () => {
      screen.getByRole("button", { name: "reboot" }).click();
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });
    expect(fetch).toHaveBeenCalledTimes(1);

    rendered.unmount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000);
    });

    expect(fetch).toHaveBeenCalledTimes(1);
  });
});
