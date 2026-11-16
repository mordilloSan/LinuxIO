import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useEffect, useRef } from "react";

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

/**
 * The Trash's jobs: restore, restore into a folder, and delete for good.
 * Each is a job that holds no dialog, any number run at once, and each
 * reports its own result. A restore whose names are taken asks with the
 * paste prompt and sends those items again with the answers, once.
 */
export function useTrashJobs({
  resolveCollisions,
}: UseTrashJobsParams): TrashJobs {
  const toast = useScopedToast(FILES_TOAST_META);
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  // Questions about taken names are asked one job after another: the prompt
  // holds one question, and a second would cancel the first.
  const asking = useRef<Promise<void>>(Promise.resolve());
  // The prompt is the page's that started the job. A job outlives a page,
  // and one that ends after its page is gone cannot ask with it.
  const mounted = useRef(false);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

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

  const askAndRestoreAgain = async (
    request: TrashRestoreRequest,
    taken: readonly FileBatchItemFailure[],
  ) => {
    const originals = originalPaths.get(request);
    const destinationOf = (path: string) =>
      request.destination
        ? joinPath(request.destination, request.names?.[path] ?? baseName(path))
        : (originals?.get(path) ?? "");
    // An item whose destination the page does not know cannot be asked
    // about; it is reported as the server put it.
    const unknown = taken.filter((item) => !destinationOf(item.path));
    if (unknown.length > 0) reportFailures(unknown, "restore");
    const items = taken
      .map((item) => item.path)
      .filter((path) => destinationOf(path));
    if (items.length === 0) return;
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
    const resolution = await resolveCollisions(
      items,
      destinationOf,
      request.destination ?? commonFolder(items.map(destinationOf)),
      (path) => incoming.get(path),
    );
    // Cancel and Skip leave the items in the Trash.
    if (!resolution || resolution.kept.length === 0) return;
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
  };

  const restoreMutation = linuxio.filebrowser.trash_restore.useMutation({
    onStart: (task) => void markTerminalFeedbackEmitted(task.id),
    success: (result, request) => {
      const failed = result?.failed ?? [];
      // The prompt lives in the file browser page; a job that ends after the
      // user left it reports a taken name like any other failure.
      const taken = mounted.current
        ? failed.filter((item) => item.reason === "exists")
        : [];
      const others = taken.length
        ? failed.filter((item) => item.reason !== "exists")
        : failed;
      if (others.length > 0) {
        reportFailures(others, "restore");
      } else if ((result?.succeeded ?? 0) > 0 || taken.length === 0) {
        toast.success(`Restored ${itemCount(result?.succeeded ?? 0)}`);
      }
      if (taken.length > 0) {
        asking.current = asking.current
          .then(() => askAndRestoreAgain(request, taken))
          .catch(() => {
            // A question that failed leaves its items in the Trash.
          });
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
