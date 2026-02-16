// src/hooks/usePackageUpdater.ts
import { useState, useCallback, useRef } from "react";

import {
  linuxio,
  openPackageUpdateStream,
  awaitStreamResult,
  type Stream,
} from "@/api";

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
  const cancelledRef = useRef(false);

  const { mutateAsync: installPackage } =
    linuxio.dbus.install_package.useMutation();

  const { refetch: refetchUpdatesBasic } =
    linuxio.dbus.get_updates_basic.useQuery({
      enabled: false,
    });

  const updateOne = useCallback(
    async (pkg: string) => {
      setUpdatingPackage(extractPackageName(pkg));
      setError(null);
      setStatus("Installing");

      try {
        await installPackage([pkg]);
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
    [installPackage, onComplete],
  );

  const updateAllFallback = useCallback(
    async (pkgs: string[]) => {
      const updated = new Set<string>();
      let remaining = [...pkgs];
      const failedPackages: string[] = [];

      while (remaining.length > 0) {
        const pkg = remaining[0];
        setUpdatingPackage(extractPackageName(pkg));
        setStatus("Installing");

        try {
          await installPackage([pkg]);
          updated.add(pkg);

          const totalProcessed = updated.size + failedPackages.length;
          const totalPackages =
            updated.size + failedPackages.length + remaining.length - 1;
          setProgress((totalProcessed / totalPackages) * 100);

          const { data: freshUpdates } = await refetchUpdatesBasic();
          const fresh = freshUpdates || [];

          remaining = fresh
            .map((u: { package_id: string }) => u.package_id)
            .filter(
              (id: string) => !updated.has(id) && !failedPackages.includes(id),
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
    },
    [installPackage, onComplete, refetchUpdatesBasic],
  );

  const updateAll = useCallback(
    async (packages: string[]) => {
      if (packages.length === 0) {
        console.log("No packages to update");
        return;
      }

      const stream = openPackageUpdateStream(packages);
      if (!stream) {
        // Fallback to sequential updates if stream not available
        console.warn("Stream connection not ready, using fallback");
        return updateAllFallback(packages);
      }

      setProgress(0);
      setError(null);
      setStatus("Initializing");
      setUpdatingPackage("Preparing updates...");
      cancelledRef.current = false;

      streamRef.current = stream;

      try {
        await awaitStreamResult<void, PkgUpdateProgress>(stream, {
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
                  setUpdatingPackage(extractPackageName(data.package_id));
                }
                break;
              case "status":
                if (data.status) {
                  setStatus(data.status);
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
            }
          },
          closeMessage: "Update stream closed unexpectedly",
        });

        if (cancelledRef.current) {
          throw new Error("Update cancelled");
        }

        setProgress(100);
        setUpdatingPackage(null);
        setStatus(null);
        await Promise.resolve(onComplete()).catch(() => undefined);
      } catch (err: unknown) {
        if (cancelledRef.current) {
          cancelledRef.current = false;
          throw new Error("Update cancelled");
        }

        const errorMsg = err instanceof Error ? err.message : "Update failed";
        setError(errorMsg);
        setUpdatingPackage(null);
        setStatus(null);
        throw err instanceof Error ? err : new Error(errorMsg);
      } finally {
        streamRef.current = null;
        cancelledRef.current = false;
      }
    },
    [onComplete, updateAllFallback],
  );

  const cancelUpdate = useCallback(() => {
    if (streamRef.current) {
      cancelledRef.current = true;
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
