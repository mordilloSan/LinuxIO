import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useEffect, useEffectEvent } from "react";

import {
  call,
  type FileBatchItemFailure,
  getMutationErrorMessage,
  invalidateOperationQueries,
  isTaskCancellationError,
  linuxio,
  type TrashEntry,
  type TrashRestoreRequest,
} from "@/api";
import { useScopedToast } from "@/hooks/useScopedToast";
import { batchFailureOpts } from "@/tasks/batchFailures";
import { markTerminalFeedbackEmitted } from "@/tasks/terminalTaskFeedback";
import { toastFileError } from "@/utils/linkRefusal";
import {
  baseName,
  joinPath,
  parentPath,
  stripTrailingSlash,
} from "@/utils/path";

import type { TrashDragItem } from "../-internalDrag";
import {
  askAboutTakenNames,
  settleTakenNamesPrompt,
  showTakenNamesAsker,
  type TakenNamesPrompt,
  takenNamesAskerShows,
  useNextTakenNamesPrompt,
} from "./takenNamesPrompts";
import type { ResolveCollisionsFn } from "./useFileConflicts";
import { FILES_TOAST_META } from "./useFileMutations";

interface UseTrashJobsParams {
  /** The prompt a paste asks with, for items whose name is taken. */
  resolveCollisions: ResolveCollisionsFn;
}

export interface TrashJobs {
  /** Deletes trashed items for good. */
  purge: (paths: string[]) => void;
  /**
   * Restores trashed items. `entries` are the listed items of a restore to
   * where they came from, which the question about a taken name needs.
   */
  restore: (
    request: TrashRestoreRequest,
    entries?: readonly TrashEntry[],
  ) => void;
  /** Restores items dragged out of the Trash into a folder. */
  restoreInto: (
    items: readonly TrashDragItem[],
    destinationDir: string,
  ) => void;
}

// Where each item of a restore to its original location goes back to, kept
// beside its request for the question about a taken name: the job's result
// names the trashed path only.
const originalPaths = new WeakMap<TrashRestoreRequest, Map<string, string>>();

const itemCount = (count: number) => `${count} item${count === 1 ? "" : "s"}`;

const pick = (
  record: Record<string, string> | undefined,
  paths: readonly string[],
): Record<string, string> =>
  Object.fromEntries(
    paths.flatMap((path) =>
      record?.[path] === undefined ? [] : [[path, record[path]]],
    ),
  );

// The folder the conflict prompt names: the one all destinations share.
const commonFolder = (paths: readonly string[]) =>
  paths.map(parentPath).reduce((common, folder) => {
    let shared = common;
    while (
      shared !== "/" &&
      folder !== shared &&
      !folder.startsWith(`${shared}/`)
    ) {
      shared = parentPath(shared);
    }
    return shared;
  });

// Where each item of a restore goes: into the request's folder under the
// name it gives, or back where it came from, as far as the page knows; ""
// when it does not.
const destinationsOf = (request: TrashRestoreRequest) => {
  const originals = originalPaths.get(request);
  return (path: string) =>
    request.destination
      ? joinPath(request.destination, request.names?.[path] ?? baseName(path))
      : (originals?.get(path) ?? "");
};

/**
 * The Trash's jobs: restore, restore into a folder, and delete for good.
 * Each is a job that holds no dialog, any number run at once, and each
 * reports its own result. A restore whose names are taken asks with the
 * paste prompt and sends those items again with the answers, once. The
 * question waits outside the page (`takenNamesPrompts.ts`), so a restore
 * that ends after the user left the file browser asks when it shows again.
 */
export function useTrashJobs({
  resolveCollisions,
}: UseTrashJobsParams): TrashJobs {
  const toast = useScopedToast(FILES_TOAST_META);
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const reportFailures = (failed: FileBatchItemFailure[], action: string) =>
    toastFileError(
      toast,
      navigate,
      `Failed to ${action} ${itemCount(failed.length)}`,
      batchFailureOpts(failed),
    );

  const reportError = (error: unknown, route: string, fallback: string) => {
    // A job that failed or was cancelled may have done part of its work, so
    // the lists are read again, as the route's manifest does on success.
    void invalidateOperationQueries(queryClient, route);
    // A cancel is not a failure: the task tray that cancelled it says so.
    if (isTaskCancellationError(error)) return;
    toastFileError(toast, navigate, getMutationErrorMessage(error, fallback));
  };

  // The second request of items whose name was taken. What it cannot do is
  // reported like any failure, a name taken again included: the user is
  // asked once per restore, never in a loop.
  const restoreAgain = linuxio.filebrowser.trash_restore.useMutation({
    onStart: (task) => void markTerminalFeedbackEmitted(task.id),
    success: (result) => {
      const failed = result?.failed ?? [];
      if (failed.length > 0) reportFailures(failed, "restore");
      else toast.success(`Restored ${itemCount(result?.succeeded ?? 0)}`);
    },
    error: (error) =>
      reportError(
        error,
        "filebrowser.trash_restore",
        "Failed to restore items",
      ),
  });
  const { mutate: startRestoreAgain } = restoreAgain;

  // Asks about one restore's taken names and sends the items again with the
  // answers. It answers whether the question is settled: false when this
  // page went away first, which leaves it for the next file browser.
  const askAndRestoreAgain = async (
    { destinations, request }: TakenNamesPrompt,
    active: () => boolean,
  ) => {
    const items = Object.keys(destinations);
    const destinationOf = (path: string) => destinations[path] ?? "";
    // The incoming side of the prompt, as a paste shows it. Best effort.
    const incoming = new Map<string, { modified: string; size: number }>();
    try {
      const response = await call(linuxio.filebrowser.exists_batch.route, {
        paths: items,
      });
      for (const entry of response.existing ?? []) {
        incoming.set(entry.path, entry);
      }
    } catch {
      // The prompt works without them.
    }
    if (!active()) return false;
    const resolution = await resolveCollisions(
      items,
      destinationOf,
      request.destination ?? commonFolder(items.map(destinationOf)),
      (path) => incoming.get(path),
    );
    if (!active()) return false;
    // Cancel and Skip leave the items in the Trash.
    if (!resolution || resolution.kept.length === 0) return true;
    const { keepBoth, overwrite } = resolution;
    // Two trashed items that go back under one name were asked about once.
    // Replacing with both would put the second over the first, which is then
    // gone for good: only the first replaces, the others stay in the Trash.
    const replacing = new Set<string>();
    const kept = resolution.kept.filter((path) => {
      if (keepBoth.includes(path)) return true;
      const destination = destinationOf(path);
      if (replacing.has(destination)) return false;
      replacing.add(destination);
      return true;
    });
    const left = resolution.kept.length - kept.length;
    if (left > 0) {
      toast.info(
        `${itemCount(left)} with the same name ${left === 1 ? "stays" : "stay"} in the Trash`,
      );
    }
    const retry: TrashRestoreRequest = { paths: kept };
    // `overwrite` covers every item of the request that does not keep both,
    // so it goes only here, in a request of items the user was asked about.
    if (overwrite) retry.overwrite = true;
    if (keepBoth.length > 0) retry.keepBoth = keepBoth;
    if (request.destination) {
      retry.destination = request.destination;
      if (request.names) retry.names = pick(request.names, kept);
    }
    // An item another program trashed goes back, or replaces what is there,
    // only at the destination confirmed for it: the one the user was asked
    // about before the first request.
    const confirmed = pick(request.confirmedDestinations, kept);
    // Back where it came from, a replace is confirmed for the destination
    // the user has just been asked about, which an item LinuxIO has no
    // record of trashing needs even in a folder of the user's own.
    if (overwrite && !request.destination) {
      for (const path of kept) {
        if (!keepBoth.includes(path)) confirmed[path] = destinationOf(path);
      }
    }
    if (Object.keys(confirmed).length > 0) {
      retry.confirmedDestinations = confirmed;
    }
    startRestoreAgain(retry);
    return true;
  };

  // The questions are asked one restore after another, by the file browser
  // that shows: the prompt holds one question, and a second would cancel
  // the first.
  const prompt = useNextTakenNamesPrompt();
  const ask = useEffectEvent((next: TakenNamesPrompt, active: () => boolean) =>
    askAndRestoreAgain(next, active),
  );
  useEffect(() => showTakenNamesAsker(), []);
  useEffect(() => {
    if (!prompt) return;
    let active = true;
    ask(prompt, () => active).then(
      (settled) => {
        if (settled) settleTakenNamesPrompt(prompt.id);
      },
      () => {
        // A question that failed leaves its items in the Trash.
        if (active) settleTakenNamesPrompt(prompt.id);
      },
    );
    return () => {
      active = false;
    };
  }, [prompt]);

  const restoreMutation = linuxio.filebrowser.trash_restore.useMutation({
    onStart: (task) => void markTerminalFeedbackEmitted(task.id),
    success: (result, request) => {
      const failed = result?.failed ?? [];
      // A taken name is asked about, now or when a file browser shows again.
      // An item whose destination the page does not know cannot be, and is
      // reported as the server put it.
      const destinationOf = destinationsOf(request);
      const taken = failed.filter(
        (item) => item.reason === "exists" && destinationOf(item.path),
      );
      const others = failed.filter((item) => !taken.includes(item));
      if (others.length > 0) {
        reportFailures(others, "restore");
      } else if ((result?.succeeded ?? 0) > 0 || taken.length === 0) {
        toast.success(`Restored ${itemCount(result?.succeeded ?? 0)}`);
      }
      askAboutTakenNames(
        request,
        Object.fromEntries(
          taken.map((item) => [item.path, destinationOf(item.path)]),
        ),
      );
      // With no file browser showing, the question waits unseen: say so,
      // with the toast's link to the file browser that will ask it.
      if (taken.length > 0 && !takenNamesAskerShows()) {
        const one = taken.length === 1;
        toast.info(
          `${itemCount(taken.length)} ${one ? "waits" : "wait"} in the Trash`,
          {
            description: `${one ? "Its name is" : "Their names are"} taken where ${one ? "it goes" : "they go"}. The file browser asks what to do with ${one ? "it" : "them"} when it shows.`,
          },
        );
      }
    },
    error: (error) =>
      reportError(
        error,
        "filebrowser.trash_restore",
        "Failed to restore items",
      ),
  });
  const { mutate: startRestore } = restoreMutation;

  const purgeMutation = linuxio.filebrowser.trash_purge.useMutation({
    onStart: (task) => void markTerminalFeedbackEmitted(task.id),
    success: (result) => {
      const failed = result?.failed ?? [];
      if (failed.length > 0) reportFailures(failed, "delete");
      else toast.success(`Deleted ${itemCount(result?.succeeded ?? 0)}`);
    },
    error: (error) =>
      reportError(error, "filebrowser.trash_purge", "Failed to delete items"),
  });
  const { mutate: startPurge } = purgeMutation;

  const restore = (
    request: TrashRestoreRequest,
    entries?: readonly TrashEntry[],
  ) => {
    if (request.paths.length === 0) return;
    if (!request.destination && entries) {
      originalPaths.set(
        request,
        new Map(entries.map((entry) => [entry.path, entry.originalPath])),
      );
    }
    startRestore(request);
  };

  // The drop site has already refused items dragged out of another
  // server's Trash (acceptTrashDrop).
  const restoreInto = (
    items: readonly TrashDragItem[],
    destinationDir: string,
  ) => {
    if (items.length === 0) return;
    startRestore({
      destination: stripTrailingSlash(destinationDir),
      names: Object.fromEntries(items.map((item) => [item.path, item.name])),
      paths: items.map((item) => item.path),
    });
  };

  const purge = (paths: string[]) => {
    if (paths.length === 0) return;
    startPurge({ paths });
  };

  return { purge, restore, restoreInto };
}
