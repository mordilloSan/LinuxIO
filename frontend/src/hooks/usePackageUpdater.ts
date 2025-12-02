// src/hooks/usePackageUpdater.ts
import axios from "@/utils/axios";
import { useState } from "react";

export const usePackageUpdater = (onComplete: () => void | Promise<any>) => {
  const [updatingPackage, setUpdatingPackage] = useState<string | null>(null);
  const [progress, setProgress] = useState(0);
  const [error, setError] = useState<string | null>(null);

  const updateOne = async (pkg: string) => {
    setUpdatingPackage(pkg);
    setError(null);

    try {
      await axios.post("/updates/update", { package: pkg });
      await onComplete(); // refresh updates
    } catch (err: any) {
      const errorMsg =
        err.response?.data?.error || err.message || "Update failed";
      setError(`Failed to update ${pkg}: ${errorMsg}`);
      console.error(`Failed to update ${pkg}`, err);
      throw err; // Re-throw so UI can handle it
    } finally {
      setUpdatingPackage(null);
    }
  };

  const updateAll = async (packages: string[]) => {
    if (packages.length === 0) {
      console.log("No packages to update");
      return;
    }

    setProgress(0);
    setError(null);
    setUpdatingPackage("Initializing...");

    const updated = new Set<string>();
    let remaining = [...packages];
    let failedPackages: string[] = [];

    while (remaining.length > 0) {
      const pkg = remaining[0];
      setUpdatingPackage(pkg);

      try {
        // Use the correct endpoint
        await axios.post("/updates/update", { package: pkg });
        updated.add(pkg);

        // Update progress
        const totalProcessed = updated.size + failedPackages.length;
        const totalPackages =
          updated.size + failedPackages.length + remaining.length - 1;
        setProgress((totalProcessed / totalPackages) * 100);

        // Refresh and update the remaining list
        const res = await axios.get("/updates/packages");
        const fresh = res.data?.updates || []; // Fix: access .updates

        // Fix: use package_id instead of name
        remaining = fresh
          .map((u: any) => u.package_id)
          .filter(
            (id: string) => !updated.has(id) && !failedPackages.includes(id),
          );
      } catch (err: any) {
        console.error(`Failed to update ${pkg}`, err);
        failedPackages.push(pkg);

        // Remove failed package from remaining
        remaining = remaining.filter((p) => p !== pkg);

        // Continue with next package instead of breaking
        const errorMsg =
          err.response?.data?.error || err.message || "Unknown error";
        console.warn(`Skipping ${pkg}: ${errorMsg}`);
      }
    }

    setProgress(100);
    setUpdatingPackage(null);

    // Show summary if there were failures
    if (failedPackages.length > 0) {
      setError(
        `Updated ${updated.size} packages. Failed: ${failedPackages.length} (${failedPackages.join(", ")})`,
      );
    }

    await onComplete();
  };

  const clearError = () => setError(null);

  return {
    updatingPackage,
    updateOne,
    updateAll,
    progress,
    error,
    clearError,
  };
};
