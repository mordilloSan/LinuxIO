import { useEffect, useRef, useState, type ReactNode } from "react";
import { toast } from "sonner";

import { actionErrorMessage } from "@/api";
import { classifyRequestOutcome } from "@/api/errors";
import {
  isRequestAvailable,
  subscribeRequestAvailability,
} from "@/api/linuxio";
import { webserverResponds } from "@/api/webserver";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";
import { GAP_MD } from "@/theme/constants";
import { redirectToSignIn } from "@/utils/navigation";

import {
  PowerActionContext,
  type SettlePowerAction,
} from "./PowerActionContext";
import { setPowerActionPending } from "./powerActionPending";

// What the overlay claims. "rebooting" waits until the webserver has gone away
// and come back, "poweringOff" until the route answers. The "unconfirmed" and
// "unknown" states say what is not known, and can be closed.
type PowerActionState =
  | "rebooting"
  | "rebootUnconfirmed"
  | "poweringOff"
  | "poweredOff"
  | "powerOffUnknown"
  | null;

// A reboot stops the webserver within this long. Past it, a failed poll keeps
// the wait going, a closed connection of the app's own ends it at the sign-in
// page, and with neither the reboot cannot be confirmed. The wait for a
// webserver whose poll failed is not bounded.
export const REBOOT_CONFIRM_TIMEOUT_MS = 90_000;

// A host that goes silent without closing the connection (power cut, network
// down) never answers `control.power_off`, and the stream has no heartbeat.
// After this long the overlay stops claiming it is waiting.
export const POWER_OFF_ANSWER_TIMEOUT_MS = 90_000;

// A fast restart can go down and come back between two polls, so poll often.
const REBOOT_POLL_INTERVAL_MS = 1000;
const REBOOT_POLL_TIMEOUT_MS = 10_000;

const OVERLAY_TITLE = {
  rebooting: "Rebooting...",
  rebootUnconfirmed: "Reboot not confirmed",
  poweringOff: "Shutting Down...",
  poweredOff: "Shutting Down...",
  powerOffUnknown: "Power-off not confirmed",
} as const;

const OVERLAY_TEXT = {
  rebooting:
    "The reboot was requested. You will be redirected to sign in once the server is back online.",
  rebootUnconfirmed:
    "The server never stopped answering, so the reboot could not be confirmed. It may not have started.",
  poweringOff: "The power-off was requested. Waiting for the host to answer.",
  poweredOff: "The system is shutting down. You may close this window.",
  powerOffUnknown: "The host did not answer. The system may be shutting down.",
} as const;

export const PowerActionProvider = ({ children }: { children: ReactNode }) => {
  const [powerAction, setPowerAction] = useState<PowerActionState>(null);
  // The request the overlay belongs to; a settle of any other one is stale.
  const currentRequest = useRef<object | null>(null);

  const start = (state: "rebooting" | "poweringOff"): SettlePowerAction => {
    const request = {};
    currentRequest.current = request;
    setPowerAction(state);
    return (error?: unknown) => {
      if (currentRequest.current !== request) return;
      if (error !== undefined && classifyRequestOutcome(error) !== "unknown") {
        // The host said it did not do it, or nothing was sent.
        currentRequest.current = null;
        setPowerAction(null);
        toast.error(actionErrorMessage(error, "The request failed"));
        return;
      }
      // A reboot waits for the webserver, not for the route's answer.
      setPowerAction((current) =>
        current !== "poweringOff"
          ? current
          : error === undefined
            ? "poweredOff"
            : "powerOffUnknown",
      );
    };
  };
  const triggerReboot = () => start("rebooting");
  const triggerPowerOff = () => start("poweringOff");
  const closeOverlay = () => {
    currentRequest.current = null;
    setPowerAction(null);
  };

  // While an overlay is up the session ends with the host, and AuthProvider
  // must leave that exit to the overlay.
  const overlayUp = powerAction !== null;
  useEffect(() => {
    if (!overlayUp) return;
    setPowerActionPending(true);
    return () => setPowerActionPending(false);
  }, [overlayUp]);

  // A power-off the host never answers is not claimed as under way for ever.
  useEffect(() => {
    if (powerAction !== "poweringOff") return;
    const timeout = setTimeout(
      () => setPowerAction("powerOffUnknown"),
      POWER_OFF_ANSWER_TIMEOUT_MS,
    );
    return () => clearTimeout(timeout);
  }, [powerAction]);

  // After a reboot the old webserver must be seen going away, by a poll that
  // fails (or answers with an error status), before a later answer means the
  // host is back; the old one still answers while it shuts down. A poll that
  // merely ran out of time proves nothing. The app's own connection closing
  // only matters at the bound: the session is gone then, so sign-in is where
  // the page belongs once the webserver answers.
  useEffect(() => {
    if (powerAction !== "rebooting") return;

    let cancelled = false;
    let pollFailed = false;
    let connectionClosed = false;
    let boundReached = false;
    let pollInFlight = false;
    let pollTimeout: ReturnType<typeof setTimeout> | null = null;
    let requestTimeout: ReturnType<typeof setTimeout> | null = null;
    let requestController: AbortController | null = null;
    let poll: () => Promise<void>;

    const unsubscribe = subscribeRequestAvailability(() => {
      if (!isRequestAvailable()) connectionClosed = true;
    });

    const schedulePoll = () => {
      pollTimeout = setTimeout(poll, REBOOT_POLL_INTERVAL_MS);
    };

    // Nothing showed the webserver going away: the reboot cannot be confirmed.
    const unconfirmed = () => boundReached && !pollFailed && !connectionClosed;

    poll = async () => {
      pollInFlight = true;
      const controller = new AbortController();
      requestController = controller;
      let timedOut = false;
      requestTimeout = setTimeout(() => {
        timedOut = true;
        controller.abort();
      }, REBOOT_POLL_TIMEOUT_MS);
      let answer: "up" | "down" | "unknown" = "unknown";
      try {
        answer = (await webserverResponds(controller.signal)) ? "up" : "down";
      } catch {
        if (!timedOut) answer = "down";
      }
      clearTimeout(requestTimeout);
      requestTimeout = null;
      requestController = null;
      pollInFlight = false;
      if (cancelled) return;
      if (answer === "down") pollFailed = true;
      if (
        answer === "up" &&
        (pollFailed || (boundReached && connectionClosed))
      ) {
        redirectToSignIn();
        return;
      }
      if (unconfirmed()) {
        setPowerAction("rebootUnconfirmed");
        return;
      }
      schedulePoll();
    };

    schedulePoll();
    // A poll still in flight when the bound is reached decides it when it ends.
    const confirmTimeout = setTimeout(() => {
      boundReached = true;
      if (!pollInFlight && unconfirmed()) setPowerAction("rebootUnconfirmed");
    }, REBOOT_CONFIRM_TIMEOUT_MS);

    return () => {
      cancelled = true;
      unsubscribe();
      clearTimeout(confirmTimeout);
      if (pollTimeout !== null) clearTimeout(pollTimeout);
      if (requestTimeout !== null) clearTimeout(requestTimeout);
      requestController?.abort();
    };
  }, [powerAction]);

  return (
    <PowerActionContext.Provider value={{ triggerReboot, triggerPowerOff }}>
      {children}
      {powerAction !== null ? (
        <div
          style={{
            position: "fixed",
            inset: 0,
            zIndex: "calc(var(--app-z-dialog) + 1)",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            padding: "24px",
            color: "white",
            background: "rgba(0, 0, 0, 0.52)",
            backdropFilter: "blur(16px)",
          }}
        >
          <div
            style={{
              display: "flex",
              flexDirection: "column",
              alignItems: "center",
              gap: GAP_MD,
              textAlign: "center",
              paddingLeft: "12px",
              paddingRight: "12px",
            }}
          >
            <AppTypography component="div" gutterBottom variant="h5">
              {OVERLAY_TITLE[powerAction]}
            </AppTypography>
            <AppTypography style={{ opacity: 0.8 }} variant="body1">
              {OVERLAY_TEXT[powerAction]}
            </AppTypography>
            {powerAction === "rebooting" && <ComponentLoader />}
            {(powerAction === "rebootUnconfirmed" ||
              powerAction === "powerOffUnknown") && (
              <AppButton
                color="inherit"
                onClick={closeOverlay}
                variant="outlined"
              >
                Close
              </AppButton>
            )}
          </div>
        </div>
      ) : null}
    </PowerActionContext.Provider>
  );
};
