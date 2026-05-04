// src/hooks/usePackageUpdater.ts
import { useState, useCallback, useRef } from "react";

import { linuxio, openJobAttachStream, type Stream } from "@/api";
import { useStreamResult } from "@/hooks/useStreamResult";

const MIN_PROGRESS_VISIBLE_MS = 1500;
const JOB_TYPE_PACKAGE_UPDATE = "package.update";

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => {
    window.setTimeout(resolve, ms);
  });
}

async function ensureMinimumVisible(startedAtMs: number): Promise<void> {
  const elapsed = Date.now() - startedAtMs;
  const remaining = MIN_PROGRESS_VISIBLE_MS - elapsed;
  if (remaining > 0) {
    await sleep(remaining);
  }
}

// Progress event types from backend
interface PkgUpdateProgress {
  type: "item_progress" | "package" | "status" | "percentage" | "message";
  package_id?: string;
  package_summary?: string;
  status?: string;
  message?: string;
  status_code?: number;
  info_code?: number;
  percentage?: number;
  item_pct?: number;
}

// Extract package name from package ID (e.g., "nginx;1.24.0-1ubuntu1;amd64;ubuntu" -> "nginx")
function extractPackageName(packageId: string): string {
  const parts = packageId.split(";");
  return parts[0] || packageId;
}

export const usePackageUpdater = (onComplete: () => unknown) => {
  const [updatingPackage, setUpdatingPackage] = useState<string | null>(null);
  const [progress, setProgress] = useState(0);
  const [status, setStatus] = useState<string | null>(null);
  const [eventLog, setEventLog] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);
  const streamRef = useRef<Stream | null>(null);
  const jobIdRef = useRef<string | null>(null);
  const cancelledRef = useRef(false);
  const { run: runStreamResult } = useStreamResult();

  const { mutateAsync: installPackage } =
    linuxio.dbus.install_package.useMutation();

  const appendEvent = useCallback((message: string) => {
    const trimmed = message.trim();
    if (!trimmed) {
      return;
    }

    setEventLog((previous) => {
      if (previous[previous.length - 1] === trimmed) {
        return previous;
      }
      const next = [...previous, trimmed];
      return next.slice(-8);
    });
  }, []);

  const updateOne = useCallback(
    async (pkg: string) => {
      const startedAtMs = Date.now();
      setEventLog([]);
      setUpdatingPackage(extractPackageName(pkg));
      setError(null);
      setStatus("Installing");
      appendEvent(`Installing: ${extractPackageName(pkg)}`);

      try {
        await installPackage([pkg]);
        await onComplete();
      } catch (err: unknown) {
        const errorMsg = err instanceof Error ? err.message : "Update failed";
        setError(`Failed to update ${extractPackageName(pkg)}: ${errorMsg}`);
        console.error(`Failed to update ${pkg}`, err);
      } finally {
        await ensureMinimumVisible(startedAtMs);
        setUpdatingPackage(null);
        setStatus(null);
      }
    },
    [appendEvent, installPackage, onComplete],
  );

  const updateAll = useCallback(
    async (packages: string[]) => {
      const startedAtMs = Date.now();
      if (packages.length === 0) {
        console.log("No packages to update");
        return;
      }

      setProgress(0);
      setEventLog([]);
      setError(null);
      setStatus("Initializing");
      setUpdatingPackage("Preparing updates...");
      appendEvent("Initializing update transaction");
      cancelledRef.current = false;

      try {
        const job = await linuxio.jobs.start.call(
          JOB_TYPE_PACKAGE_UPDATE,
          ...packages,
        );
        jobIdRef.current = job.id;

        await runStreamResult<void, PkgUpdateProgress>({
          open: () => openJobAttachStream(job.id),
          closeOnAbort: "none",
          onOpen: (stream) => {
            streamRef.current = stream;
          },
          onProgress: (data) => {
            switch (data.type) {
              case "item_progress":
                if (data.package_id) {
                  setUpdatingPackage(extractPackageName(data.package_id));
                }
                if (data.status) {
                  setStatus(data.status);
                }
                if (data.item_pct !== undefined) {
                  setProgress(data.item_pct);
                }
                break;
              case "package":
                if (data.package_id) {
                  const packageName = extractPackageName(data.package_id);
                  setUpdatingPackage(packageName);
                  if (data.status) {
                    appendEvent(`${data.status}: ${packageName}`);
                  }
                }
                if (data.status) {
                  setStatus(data.status);
                }
                break;
              case "status":
                if (data.status) {
                  setStatus(data.status);
                  appendEvent(data.status);
                }
                if (data.percentage !== undefined) {
                  setProgress(data.percentage);
                }
                break;
              case "percentage":
                if (data.percentage !== undefined) {
                  setProgress(data.percentage);
                }
                break;
              case "message":
                if (data.message) {
                  setStatus(data.message);
                  appendEvent(data.message);
                } else if (data.status) {
                  setStatus(data.status);
                  appendEvent(data.status);
                }
                break;
            }
          },
          closeMessage: "Update stream closed unexpectedly",
        });

        if (cancelledRef.current) {
          return;
        }

        setProgress(100);
        setStatus("Finished");
        appendEvent("Finished");
        await ensureMinimumVisible(startedAtMs);
        setUpdatingPackage(null);
        setStatus(null);
        await Promise.resolve(onComplete()).catch(() => undefined);
      } catch (err: unknown) {
        if (cancelledRef.current) {
          cancelledRef.current = false;
          return;
        }

        const errorMsg = err instanceof Error ? err.message : "Update failed";
        setError(errorMsg);
        setUpdatingPackage(null);
        setStatus(null);
      } finally {
        streamRef.current = null;
        jobIdRef.current = null;
        cancelledRef.current = false;
      }
    },
    [appendEvent, onComplete, runStreamResult],
  );

  const cancelUpdate = useCallback(() => {
    if (streamRef.current || jobIdRef.current) {
      cancelledRef.current = true;
      streamRef.current?.abort();
      streamRef.current = null;
      if (jobIdRef.current) {
        void linuxio.jobs.cancel.call(jobIdRef.current).catch(() => undefined);
        jobIdRef.current = null;
      }
      setUpdatingPackage(null);
      setStatus(null);
      setError("Update cancelled");
    }
  }, []);

  const clearError = useCallback(() => setError(null), []);

  return {
    updatingPackage,
    updateOne,
    updateAll,
    cancelUpdate,
    progress,
    status,
    eventLog,
    error,
    clearError,
  };
};
