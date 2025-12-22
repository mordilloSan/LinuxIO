// src/hooks/usePackageUpdater.ts
import { useState, useCallback, useRef } from "react";

import { streamApi } from "@/utils/streamApi";
import { getStreamMux, Stream, encodeString } from "@/utils/StreamMultiplexer";

// Stream type for package updates (must match backend ipc.StreamTypePkgUpdate)
const STREAM_TYPE_PKG_UPDATE = "pkg-update";

// Progress event types from backend
interface PkgUpdateProgress {
  type: "item_progress" | "package" | "status" | "percentage";
  package_id?: string;
  status?: string;
  status_code?: number;
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
  const [error, setError] = useState<string | null>(null);
  const streamRef = useRef<Stream | null>(null);

  const updateOne = useCallback(
    async (pkg: string) => {
      setUpdatingPackage(extractPackageName(pkg));
      setError(null);
      setStatus("Installing");

      try {
        await streamApi.post("dbus", "InstallPackage", null, [pkg]);
        await onComplete();
      } catch (err: unknown) {
        const errorMsg = err instanceof Error ? err.message : "Update failed";
        setError(`Failed to update ${extractPackageName(pkg)}: ${errorMsg}`);
        console.error(`Failed to update ${pkg}`, err);
        throw err;
      } finally {
        setUpdatingPackage(null);
        setStatus(null);
      }
    },
    [onComplete],
  );

  const updateAll = useCallback(
    async (packages: string[]) => {
      if (packages.length === 0) {
        console.log("No packages to update");
        return;
      }

      const mux = getStreamMux();
      if (!mux || mux.status !== "open") {
        // Fallback to sequential updates if stream not available
        console.warn("Stream connection not ready, using fallback");
        return updateAllFallback(packages);
      }

      setProgress(0);
      setError(null);
      setStatus("Initializing");
      setUpdatingPackage("Preparing updates...");

      // Build payload: stream type followed by package IDs (null-separated)
      const payloadParts = [STREAM_TYPE_PKG_UPDATE, ...packages];
      const payload = encodeString(payloadParts.join("\0"));

      const stream = mux.openStream(STREAM_TYPE_PKG_UPDATE, payload);
      if (!stream) {
        setError("Failed to open update stream");
        return;
      }

      streamRef.current = stream;

      return new Promise<void>((resolve, reject) => {
        stream.onProgress = (progressData: unknown) => {
          const data = progressData as PkgUpdateProgress;

          switch (data.type) {
            case "item_progress":
              // Per-item progress (most detailed)
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
              // Package being processed
              if (data.package_id) {
                setUpdatingPackage(extractPackageName(data.package_id));
              }
              break;

            case "status":
              // Overall status update
              if (data.status) {
                setStatus(data.status);
              }
              if (data.percentage !== undefined) {
                setProgress(data.percentage);
              }
              break;

            case "percentage":
              // Overall percentage
              if (data.percentage !== undefined) {
                setProgress(data.percentage);
              }
              break;
          }
        };

        stream.onResult = (result) => {
          streamRef.current = null;

          if (result.status === "ok") {
            setProgress(100);
            setUpdatingPackage(null);
            setStatus(null);
            // Handle both sync and async onComplete
            Promise.resolve(onComplete())
              .then(() => resolve())
              .catch(() => resolve());
          } else {
            const errorMsg = result.error || "Update failed";
            setError(errorMsg);
            setUpdatingPackage(null);
            setStatus(null);
            reject(new Error(errorMsg));
          }
        };

        stream.onClose = () => {
          streamRef.current = null;
          // If closed without result, treat as error
          if (status !== null) {
            setError("Update stream closed unexpectedly");
            setUpdatingPackage(null);
            setStatus(null);
            reject(new Error("Stream closed unexpectedly"));
          }
        };
      });

      // Fallback function for when streaming isn't available
      async function updateAllFallback(pkgs: string[]) {
        const updated = new Set<string>();
        let remaining = [...pkgs];
        const failedPackages: string[] = [];

        while (remaining.length > 0) {
          const pkg = remaining[0];
          setUpdatingPackage(extractPackageName(pkg));
          setStatus("Installing");

          try {
            await streamApi.post("dbus", "InstallPackage", null, [pkg]);
            updated.add(pkg);

            const totalProcessed = updated.size + failedPackages.length;
            const totalPackages =
              updated.size + failedPackages.length + remaining.length - 1;
            setProgress((totalProcessed / totalPackages) * 100);

            const freshUpdates = await streamApi.get<{ package_id: string }[]>(
              "dbus",
              "GetUpdatesBasic",
            );
            const fresh = freshUpdates || [];

            remaining = fresh
              .map((u) => u.package_id)
              .filter(
                (id: string) =>
                  !updated.has(id) && !failedPackages.includes(id),
              );
          } catch (err) {
            console.error(`Failed to update ${pkg}`, err);
            failedPackages.push(pkg);
            remaining = remaining.filter((p) => p !== pkg);
          }
        }

        setProgress(100);
        setUpdatingPackage(null);
        setStatus(null);

        if (failedPackages.length > 0) {
          setError(
            `Updated ${updated.size} packages. Failed: ${failedPackages.length} (${failedPackages.map(extractPackageName).join(", ")})`,
          );
        }

        await onComplete();
      }
    },
    [onComplete, status],
  );

  const cancelUpdate = useCallback(() => {
    if (streamRef.current) {
      streamRef.current.abort();
      streamRef.current = null;
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
    error,
    clearError,
  };
};
