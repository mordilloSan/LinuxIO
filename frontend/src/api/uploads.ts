import linuxio from "./generated/client";
import { openJobDataStream } from "./linuxio";
import { LinuxIOError } from "./linuxio-core";
import { streamWriteChunks, waitForStreamResult } from "./stream-helpers";

export interface UploadContentOptions {
  chunkSize?: number;
  /** Replace an existing file. Uploads never overwrite unless told to. */
  overwrite?: boolean;
  signal?: AbortSignal;
}

/**
 * Save in-memory content to a path: starts a `filebrowser.upload` job and
 * pushes the bytes over the job's data stream, resolving once the job
 * reports the write complete. This is the API-layer flow behind editor
 * saves; call sites own toasts and cache refreshes.
 */
export async function uploadContent(
  targetPath: string,
  data: Uint8Array,
  options: UploadContentOptions = {},
): Promise<void> {
  const job = await linuxio.filebrowser.upload({
    targetPath,
    size: String(data.length),
    overwrite: options.overwrite,
  });

  const stream = openJobDataStream(job.id, 0);
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
