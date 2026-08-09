import linuxio from "./generated/client";
import type { TaskSnapshot } from "./generated/linuxio-types";
import { openTaskDataStream } from "./linuxio";
import { LinuxIOError } from "./linuxio-core";
import { streamWriteChunks, waitForStreamResult } from "./stream-helpers";
import { isTerminalTaskState, terminalSnapshotOrThrow } from "./tasks";

export interface UploadContentOptions {
  chunkSize?: number;
  /**
   * Called with the Task snapshot as soon as the upload Task exists. Callers
   * surface the save outcome themselves, so they use this to claim the Task's
   * terminal feedback before the global background-Tasks watcher reports the
   * same failure a second time.
   */
  onTaskStart?: (task: TaskSnapshot) => void;
  /** Replace an existing file. Uploads never overwrite unless told to. */
  overwrite?: boolean;
  signal?: AbortSignal;
}

/**
 * Save in-memory content to a path: starts a `filebrowser.upload` Task and
 * pushes the bytes over the Task's data stream, resolving once the Task
 * reports the write complete. This is the API-layer flow behind editor
 * saves; call sites own toasts and cache refreshes.
 */
export async function uploadContent(
  targetPath: string,
  data: Uint8Array,
  options: UploadContentOptions = {},
): Promise<void> {
  const task = await linuxio.filebrowser.upload({
    targetPath,
    size: String(data.length),
    overwrite: options.overwrite,
  });
  options.onTaskStart?.(task);

  // A conflict (409 "destination already exists") fails the Task before any
  // transfer state exists, and the start reply already carries that terminal
  // snapshot. Throw the structured error here: attaching a data stream to a
  // dead Task would misreport it as a 404 "transfer task not ready".
  if (isTerminalTaskState(task.state)) {
    terminalSnapshotOrThrow(task);
    return;
  }

  const stream = openTaskDataStream(task.id, 0);
  if (!stream) {
    throw new LinuxIOError("Failed to open save stream", "stream_unavailable");
  }

  const completion = waitForStreamResult<void>(stream, {
    closeMessage: "Stream closed unexpectedly",
    signal: options.signal,
  });

  try {
    await streamWriteChunks(stream, data, {
      chunkSize: options.chunkSize,
      yieldMs: 0,
      signal: options.signal,
    });
  } catch (writeError) {
    if (stream.status === "open" || stream.status === "opening") {
      stream.abort();
    }
    await completion.catch(() => undefined);
    throw writeError;
  }

  await completion;
}
