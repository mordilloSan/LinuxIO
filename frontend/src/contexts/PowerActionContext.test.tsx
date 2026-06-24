import { beforeEach, describe, expect, it, vi } from "vitest";

import { PowerActionProvider } from "@/contexts/PowerActionContext";
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
});
