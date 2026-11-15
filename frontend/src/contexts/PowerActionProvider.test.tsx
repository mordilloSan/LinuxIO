import { beforeEach, describe, expect, it, vi } from "vitest";

import { LinuxIOError } from "@/api/errors";
import { isPowerActionPending } from "@/contexts/powerActionPending";
import {
  POWER_OFF_ANSWER_TIMEOUT_MS,
  PowerActionProvider,
  REBOOT_CONFIRM_TIMEOUT_MS,
} from "@/contexts/PowerActionProvider";
import usePowerAction from "@/hooks/usePowerAction";
import { act, render, screen } from "@/test/render";

const mocks = vi.hoisted(() => ({
  muxAvailable: true,
  muxListeners: new Set<() => void>(),
  redirectToSignIn: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("@/utils/navigation", () => ({
  redirectToSignIn: mocks.redirectToSignIn,
}));

vi.mock("sonner", async (importOriginal) => ({
  ...(await importOriginal<typeof import("sonner")>()),
  toast: { error: mocks.toastError },
}));

// The app's own connection, which a host going down closes.
vi.mock("@/api/linuxio", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/linuxio")>()),
  isRequestAvailable: () => mocks.muxAvailable,
  subscribeRequestAvailability: (listener: () => void) => {
    mocks.muxListeners.add(listener);
    return () => mocks.muxListeners.delete(listener);
  },
}));

const closeConnection = () => {
  mocks.muxAvailable = false;
  mocks.muxListeners.forEach((listener) => listener());
};

// The settle function of each request the probe started, oldest first.
let settles: Array<(error?: unknown) => void> = [];

function Probe() {
  const { triggerPowerOff, triggerReboot } = usePowerAction();
  return (
    <div>
      <button onClick={() => settles.push(triggerReboot())}>reboot</button>
      <button onClick={() => settles.push(triggerPowerOff())}>power off</button>
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

const click = (name: string) =>
  act(async () => screen.getByRole("button", { name }).click());
const settle = (index: number, error?: unknown) =>
  act(async () => settles[index](error));
const advance = (ms: number) =>
  act(async () => vi.advanceTimersByTimeAsync(ms));
const respond = (ok: boolean) => ({ ok }) as Response;

describe("PowerActionProvider", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.stubGlobal("fetch", vi.fn());
    settles = [];
    mocks.muxAvailable = true;
    mocks.muxListeners.clear();
  });

  describe("power-off", () => {
    it("shows the overlay as requested, without starting reboot polling", async () => {
      vi.useFakeTimers();
      renderProvider();

      await click("power off");
      await advance(10_000);

      expect(screen.getByText("Shutting Down...")).toBeInTheDocument();
      expect(
        screen.getByText(
          "The power-off was requested. Waiting for the host to answer.",
        ),
      ).toBeInTheDocument();
      expect(fetch).not.toHaveBeenCalled();
    });

    it("claims the shutdown only after the route answers", async () => {
      renderProvider();
      await click("power off");
      await settle(0);

      expect(
        screen.getByText(
          "The system is shutting down. You may close this window.",
        ),
      ).toBeInTheDocument();
    });

    it.each([
      ["a lost connection", new LinuxIOError("lost", "outcome_unknown")],
      ["a stopping job host", new LinuxIOError("job host is stopping", 503)],
      ["an interrupted job", new LinuxIOError("job was interrupted", 409)],
    ])(
      "says it is not confirmed after %s, and can be closed",
      async (_name, error) => {
        renderProvider();
        await click("power off");
        await settle(0, error);

        expect(screen.getByText("Power-off not confirmed")).toBeInTheDocument();
        expect(
          screen.getByText(
            "The host did not answer. The system may be shutting down.",
          ),
        ).toBeInTheDocument();
        expect(mocks.toastError).not.toHaveBeenCalled();
        await click("Close");
        expect(
          screen.queryByText("Power-off not confirmed"),
        ).not.toBeInTheDocument();
      },
    );

    it("stops waiting for a host that goes silent", async () => {
      vi.useFakeTimers();
      renderProvider();
      await click("power off");

      await advance(POWER_OFF_ANSWER_TIMEOUT_MS - 1000);
      expect(screen.getByText("Shutting Down...")).toBeInTheDocument();
      await advance(1000);

      expect(screen.getByText("Power-off not confirmed")).toBeInTheDocument();
      await click("Close");
      // The answer that finally arrives is for a request nobody waits for.
      await settle(0);
      expect(screen.queryByText(/shutting down/i)).not.toBeInTheDocument();
    });
  });

  describe("a request that did not run", () => {
    it.each(["power off", "reboot"])(
      "takes the %s overlay down and shows the reason when the host refuses",
      async (name) => {
        renderProvider();
        await click(name);
        await settle(0, new LinuxIOError("access denied", 403));

        expect(screen.queryByText(/requested/)).not.toBeInTheDocument();
        expect(mocks.toastError).toHaveBeenCalledWith("access denied");
      },
    );

    it.each(["power off", "reboot"])(
      "takes the %s overlay down with the reason when the handler failed",
      async (name) => {
        renderProvider();
        await click(name);
        // What the bridge answers a plain handler error with: logind denying it.
        await settle(
          0,
          new LinuxIOError("failed to call Reboot: access denied", 500),
        );

        expect(screen.queryByText(/requested/)).not.toBeInTheDocument();
        expect(mocks.toastError).toHaveBeenCalledWith(
          "failed to call Reboot: access denied",
        );
      },
    );

    it("shows an update that waits as a sentence", async () => {
      renderProvider();
      await click("reboot");
      await settle(
        0,
        new LinuxIOError(
          "update_pending: LinuxIO update waits for running jobs",
          409,
        ),
      );

      expect(screen.queryByText("Rebooting...")).not.toBeInTheDocument();
      expect(mocks.toastError).toHaveBeenCalledWith(
        "LinuxIO update waits for running jobs",
      );
    });

    it("takes the overlay down when the request was never sent", async () => {
      renderProvider();
      await click("reboot");
      await settle(0, new LinuxIOError("not sent", "connection_unavailable"));

      expect(screen.queryByText("Rebooting...")).not.toBeInTheDocument();
      expect(mocks.toastError).toHaveBeenCalledWith("not sent");
    });

    it.each([
      ["a lost connection", new LinuxIOError("lost", "outcome_unknown")],
      ["a stopping job host", new LinuxIOError("job host unavailable", 503)],
      ["a gateway timeout", new LinuxIOError("timed out", 504)],
      ["an interrupted job", new LinuxIOError("job was interrupted", 409)],
    ])("keeps the reboot overlay up after %s", async (_name, error) => {
      renderProvider();
      await click("reboot");
      await settle(0, error);

      expect(screen.getByText("Rebooting...")).toBeInTheDocument();
      expect(mocks.toastError).not.toHaveBeenCalled();
    });

    it("ignores the answer of an earlier request", async () => {
      renderProvider();
      await click("reboot");
      await click("power off");

      // The reboot's refusal arrives late, with the power-off overlay up.
      await settle(0, new LinuxIOError("access denied", 403));
      expect(screen.getByText("Shutting Down...")).toBeInTheDocument();
      expect(mocks.toastError).not.toHaveBeenCalled();
      await settle(0);
      expect(
        screen.getByText(
          "The power-off was requested. Waiting for the host to answer.",
        ),
      ).toBeInTheDocument();

      await settle(1);
      expect(
        screen.getByText(
          "The system is shutting down. You may close this window.",
        ),
      ).toBeInTheDocument();
    });
  });

  it("tells AuthProvider a power action is pending while the overlay is up", async () => {
    renderProvider();
    expect(isPowerActionPending()).toBe(false);

    await click("power off");
    expect(isPowerActionPending()).toBe(true);
    await settle(0, new LinuxIOError("lost", "outcome_unknown"));
    expect(isPowerActionPending()).toBe(true);

    await click("Close");
    expect(isPowerActionPending()).toBe(false);
  });

  describe("reboot", () => {
    it("shows the overlay as requested and polls every second", async () => {
      vi.useFakeTimers();
      vi.mocked(fetch).mockResolvedValue(respond(false));
      renderProvider();

      await click("reboot");

      expect(screen.getByText("Rebooting...")).toBeInTheDocument();
      expect(
        screen.getByText(
          "The reboot was requested. You will be redirected to sign in once the server is back online.",
        ),
      ).toBeInTheDocument();
      expect(fetch).not.toHaveBeenCalled();

      await advance(1000);
      expect(fetch).toHaveBeenCalledWith("/api/version", {
        cache: "no-store",
        method: "GET",
        signal: expect.any(AbortSignal),
      });
      await advance(1000);
      expect(fetch).toHaveBeenCalledTimes(2);
    });

    it("keeps retrying after transient fetch failures", async () => {
      vi.useFakeTimers();
      vi.mocked(fetch).mockRejectedValue(new Error("offline"));
      renderProvider();

      await click("reboot");
      await advance(3000);

      expect(fetch).toHaveBeenCalledTimes(3);
    });

    it("redirects to sign-in only after the webserver stopped answering", async () => {
      vi.useFakeTimers();
      vi.mocked(fetch)
        .mockResolvedValueOnce(respond(true))
        .mockResolvedValueOnce(respond(true))
        .mockRejectedValueOnce(new Error("offline"))
        .mockResolvedValue(respond(true));
      renderProvider();
      await click("reboot");

      // The old webserver still answers.
      await advance(2000);
      expect(fetch).toHaveBeenCalledTimes(2);
      expect(mocks.redirectToSignIn).not.toHaveBeenCalled();
      // It goes away.
      await advance(1000);
      expect(fetch).toHaveBeenCalledTimes(3);
      expect(mocks.redirectToSignIn).not.toHaveBeenCalled();
      // The new one answers.
      await advance(1000);
      expect(mocks.redirectToSignIn).toHaveBeenCalledTimes(1);
    });

    it("treats an error status as the webserver being down", async () => {
      vi.useFakeTimers();
      vi.mocked(fetch)
        .mockResolvedValueOnce(respond(false))
        .mockResolvedValue(respond(true));
      renderProvider();
      await click("reboot");
      await advance(2000);

      expect(mocks.redirectToSignIn).toHaveBeenCalledTimes(1);
    });

    it("does not redirect on the connection closing alone before the bound, and does at it", async () => {
      vi.useFakeTimers();
      vi.mocked(fetch).mockResolvedValue(respond(true));
      renderProvider();
      await click("reboot");

      // The session ends while the old webserver still answers.
      await advance(500);
      act(closeConnection);
      await advance(REBOOT_CONFIRM_TIMEOUT_MS - 1500);
      expect(mocks.redirectToSignIn).not.toHaveBeenCalled();
      expect(screen.getByText("Rebooting...")).toBeInTheDocument();

      // At the bound the session is gone either way: sign-in is the place.
      await advance(2000);
      expect(mocks.redirectToSignIn).toHaveBeenCalledTimes(1);
      expect(
        screen.queryByText("Reboot not confirmed"),
      ).not.toBeInTheDocument();
    });

    it("redirects at the bound when the poll in flight then answers after the connection closed", async () => {
      vi.useFakeTimers();
      const started = Date.now();
      let finishHungPoll: ((response: Response) => void) | undefined;
      vi.mocked(fetch).mockImplementation(() => {
        if (Date.now() - started >= REBOOT_CONFIRM_TIMEOUT_MS - 1500) {
          return new Promise<Response>((resolve) => {
            finishHungPoll ??= resolve;
          });
        }
        return Promise.resolve(respond(true));
      });
      renderProvider();
      await click("reboot");
      act(closeConnection);
      await advance(REBOOT_CONFIRM_TIMEOUT_MS + 1000);
      expect(mocks.redirectToSignIn).not.toHaveBeenCalled();

      await act(async () => finishHungPoll?.(respond(true)));
      expect(mocks.redirectToSignIn).toHaveBeenCalledTimes(1);
    });

    it("does not redirect while the webserver keeps answering, and says so at the bound", async () => {
      vi.useFakeTimers();
      vi.mocked(fetch).mockResolvedValue(respond(true));
      renderProvider();
      await click("reboot");
      await advance(REBOOT_CONFIRM_TIMEOUT_MS - 1000);
      expect(screen.getByText("Rebooting...")).toBeInTheDocument();

      await advance(1000);
      expect(screen.getByText("Reboot not confirmed")).toBeInTheDocument();
      expect(
        screen.getByText(
          "The server never stopped answering, so the reboot could not be confirmed. It may not have started.",
        ),
      ).toBeInTheDocument();

      // Polling stops, nothing redirects, and the overlay can be closed.
      const polls = vi.mocked(fetch).mock.calls.length;
      await advance(30_000);
      expect(fetch).toHaveBeenCalledTimes(polls);
      expect(mocks.redirectToSignIn).not.toHaveBeenCalled();
      await click("Close");
      expect(
        screen.queryByText("Reboot not confirmed"),
      ).not.toBeInTheDocument();
    });

    it("does not count a poll that ran out of time as the webserver going away", async () => {
      vi.useFakeTimers();
      vi.mocked(fetch).mockImplementationOnce(
        (_url, options) =>
          new Promise((_resolve, reject) => {
            options?.signal?.addEventListener(
              "abort",
              () => reject(options.signal?.reason),
              { once: true },
            );
          }),
      );
      vi.mocked(fetch).mockResolvedValue(respond(true));
      renderProvider();
      await click("reboot");

      await advance(1000 + 10_000 + 3000);

      expect(vi.mocked(fetch).mock.calls.length).toBeGreaterThan(2);
      expect(mocks.redirectToSignIn).not.toHaveBeenCalled();
    });

    it("lets a poll in flight at the bound decide it", async () => {
      vi.useFakeTimers();
      const started = Date.now();
      let finishHungPoll: ((error: Error) => void) | undefined;
      vi.mocked(fetch).mockImplementation(() => {
        if (Date.now() - started >= REBOOT_CONFIRM_TIMEOUT_MS - 1500) {
          return new Promise<Response>((_resolve, reject) => {
            finishHungPoll ??= reject;
          });
        }
        return Promise.resolve(respond(true));
      });
      renderProvider();
      await click("reboot");
      await advance(REBOOT_CONFIRM_TIMEOUT_MS + 1000);

      // The poll is hanging on a webserver that may just have gone away.
      expect(finishHungPoll).toBeDefined();
      expect(screen.getByText("Rebooting...")).toBeInTheDocument();

      // It fails, so the webserver did go away: keep waiting for it.
      vi.mocked(fetch).mockResolvedValue(respond(true));
      await act(async () => finishHungPoll?.(new Error("reset")));
      await advance(1000);
      expect(mocks.redirectToSignIn).toHaveBeenCalledTimes(1);
      expect(
        screen.queryByText("Reboot not confirmed"),
      ).not.toBeInTheDocument();
    });

    it("says it is not confirmed when the poll in flight at the bound answers", async () => {
      vi.useFakeTimers();
      const started = Date.now();
      let finishHungPoll: ((response: Response) => void) | undefined;
      vi.mocked(fetch).mockImplementation(() => {
        if (Date.now() - started >= REBOOT_CONFIRM_TIMEOUT_MS - 1500) {
          return new Promise<Response>((resolve) => {
            finishHungPoll ??= resolve;
          });
        }
        return Promise.resolve(respond(true));
      });
      renderProvider();
      await click("reboot");
      await advance(REBOOT_CONFIRM_TIMEOUT_MS + 1000);
      await act(async () => finishHungPoll?.(respond(true)));

      expect(screen.getByText("Reboot not confirmed")).toBeInTheDocument();
      expect(mocks.redirectToSignIn).not.toHaveBeenCalled();
    });

    it("keeps waiting without a bound once the webserver has stopped answering", async () => {
      vi.useFakeTimers();
      vi.mocked(fetch).mockRejectedValue(new Error("offline"));
      renderProvider();
      await click("reboot");
      await advance(REBOOT_CONFIRM_TIMEOUT_MS * 2);

      expect(screen.getByText("Rebooting...")).toBeInTheDocument();
      expect(mocks.redirectToSignIn).not.toHaveBeenCalled();
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
      await click("reboot");
      await advance(1000);
      expect(signals).toHaveLength(1);
      expect(signals[0].aborted).toBe(false);
      await advance(10_000);
      expect(signals[0].aborted).toBe(true);
      await advance(1000);
      expect(signals).toHaveLength(2);
      expect(signals[1].aborted).toBe(false);
      unmount();
      expect(signals[1].aborted).toBe(true);
      expect(mocks.muxListeners.size).toBe(0);
      await advance(30_000);
      expect(fetch).toHaveBeenCalledTimes(2);
    });

    it("stops scheduled retries when the provider unmounts", async () => {
      vi.useFakeTimers();
      vi.mocked(fetch).mockResolvedValue(respond(false));
      const rendered = renderProvider();
      await click("reboot");
      await advance(1000);
      expect(fetch).toHaveBeenCalledTimes(1);

      rendered.unmount();
      await advance(3000);

      expect(fetch).toHaveBeenCalledTimes(1);
    });
  });
});
