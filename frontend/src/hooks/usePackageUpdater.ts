// src/hooks/usePackageUpdater.ts
import axios from "@/utils/axios";
import { useState } from "react";

export const usePackageUpdater = (onComplete: () => void) => {
  const [updatingPackage, setUpdatingPackage] = useState<string | null>(null);
  const [progress, setProgress] = useState(0);

  const updateOne = async (pkg: string) => {
    setUpdatingPackage(pkg);
    await axios.post("/updates/update", { package: pkg });
    onComplete(); // refresh updates
    setUpdatingPackage(null);
  };

  const updateAll = async (packages: string[]) => {
    setProgress(0);
    setUpdatingPackage("");

    const updated = new Set<string>();
    let remaining = [...packages];

    while (remaining.length > 0) {
      const pkg = remaining[0];
      setUpdatingPackage(pkg);

      try {
        await axios.post("/system/update", { package: pkg });
        updated.add(pkg);

        // Refresh and update the remaining list
        const res = await axios.get("/updates/packages");
        const fresh = res.data as { name: string }[];
        remaining = fresh
          .map((u) => u.name)
          .filter((name) => !updated.has(name));

        setProgress((updated.size / (updated.size + remaining.length)) * 100);
      } catch (err) {
        console.error(`Failed to update ${pkg}`, err);
        break;
      }
    }

    setUpdatingPackage(null);
    await onComplete();
  };

  return {
    updatingPackage,
    updateOne,
    updateAll,
    progress,
  };
};
