import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";

import { openChannel, useStreamMux } from "@/api";
import { useLiveStream } from "@/hooks/useLiveStream";

import { markListingStale } from "./useListingInvalidation";

/** The pauses before a watch that ended is opened again; the last repeats. */
const REOPEN_PAUSES_MS = [1000, 2000, 5000, 30_000] as const;

/** A watch that ran this long ended for a new reason: its pauses start over. */
const SETTLED_WATCH_MS = 30_000;

/**
 * The refusals that will not change by asking again: a path that is not a
 * folder, a folder the user may not read, one that is not there, and the
 * bridge's cap. Anything else (an inotify limit, a bridge restarting) is
 * asked again after the pauses.
 */
const FINAL_REFUSALS = new Set([400, 403, 404, 409]);

/** The types of the JSON lines the watch sends as data. */
function frameTypes(text: string): string[] {
  const types: string[] = [];
  for (const line of text.split("\n")) {
    if (!line) continue;
    try {
      const frame = JSON.parse(line) as { type?: unknown };
      if (typeof frame.type === "string") types.push(frame.type);
    } catch {
      // Not a frame of this Channel; nothing to do.
    }
  }
  return types;
}

/**
 * Keeps the listing of the folder the page shows up to date while it shows
 * it: the bridge watches that one folder (`filebrowser.watch`) and says when
 * its entries change, and the listing is marked stale and read again. The
 * watch is opened again after it ends, and stops only when the bridge refuses
 * it for a reason that will not change. After the folder was gone a 404 is
 * asked again through the pauses, since the folder may be made again
 * (`rm -rf dist`, then a build). The bridge says `watching` once a watch is
 * on; after a watch that was lost the listing is read again then, since
 * changes in between were not seen, and a refused watch causes no read. The
 * frames are data, never progress, which the page counts as the user's
 * activity.
 * `path` null watches nothing. See docs/filebrowser-live-listing.md.
 */
export function useFolderWatch(path: string | null) {
  const queryClient = useQueryClient();
  const { isOpen: muxIsOpen } = useStreamMux();
  const { openStream, closeStream } = useLiveStream();
  // The folder whose watch ended while it was still shown, kept across the
  // websocket closing and coming back, so the next watch of it catches up.
  const lostWatchRef = useRef<string | null>(null);

  useEffect(() => {
    if (!muxIsOpen || path === null) return;
    const folder = path;
    let reopenTimer: number | undefined;
    let failures = 0;
    let finished = false;
    let afterGone = false;
    let missedWhileHidden = false;

    const markStale = () => {
      const hidden = document.visibilityState === "hidden";
      missedWhileHidden ||= hidden;
      markListingStale(queryClient, folder, !hidden);
    };

    const handleVisibilityChange = () => {
      if (document.visibilityState !== "visible" || !missedWhileHidden) return;
      missedWhileHidden = false;
      markListingStale(queryClient, folder, true);
    };

    const scheduleReopen = () => {
      const pause =
        REOPEN_PAUSES_MS[Math.min(failures, REOPEN_PAUSES_MS.length - 1)];
      failures += 1;
      reopenTimer = window.setTimeout(() => {
        reopenTimer = undefined;
        open();
      }, pause);
    };

    const handleFrame = (type: string) => {
      switch (type) {
        case "watching":
          // The watch is on: catch up on what a lost one did not see.
          afterGone = false;
          if (lostWatchRef.current === folder) markStale();
          lostWatchRef.current = null;
          break;
        case "changed":
          markStale();
          break;
        case "gone":
          // The listing is read and the Channel's end reopens the watch
          // after the first pause.
          afterGone = true;
          failures = 0;
          markStale();
          break;
      }
    };

    function open() {
      const openedAt = Date.now();
      openStream({
        open: () => openChannel("filebrowser.watch", { path: folder }),
        onOpenError: scheduleReopen,
        onText: (text) => {
          for (const type of frameTypes(text)) handleFrame(type);
        },
        onResult: (result) => {
          if (result.status !== "error") return;
          const code = Number(result.code);
          // After "gone", the folder may be made again: a 404 is asked
          // again through the pauses, and the one after the last ends it.
          const mayComeBack =
            code === 404 && afterGone && failures < REOPEN_PAUSES_MS.length;
          if (FINAL_REFUSALS.has(code) && !mayComeBack) finished = true;
        },
        onClose: () => {
          if (finished) return;
          lostWatchRef.current = folder;
          if (Date.now() - openedAt >= SETTLED_WATCH_MS) failures = 0;
          scheduleReopen();
        },
      });
    }

    open();
    document.addEventListener("visibilitychange", handleVisibilityChange);
    return () => {
      document.removeEventListener("visibilitychange", handleVisibilityChange);
      window.clearTimeout(reopenTimer);
      closeStream();
    };
  }, [closeStream, muxIsOpen, openStream, path, queryClient]);
}
