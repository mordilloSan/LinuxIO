import { useEffect, useState } from "react";

interface UpdateInfo {
  available: boolean;
  current_version: string;
  latest_version?: string;
  release_url?: string;
}

export const useUpdateInfo = () => {
  const [updateInfo, setUpdateInfo] = useState<UpdateInfo | null>(null);

  useEffect(() => {
    // Load from sessionStorage (set during login)
    const stored = sessionStorage.getItem("update_info");
    if (stored) {
      try {
        const parsed = JSON.parse(stored);
        setUpdateInfo(parsed);
      } catch (error) {
        console.error("Failed to parse update info:", error);
      }
    }
  }, []);

  const dismissUpdate = () => {
    setUpdateInfo(null);
    sessionStorage.removeItem("update_info");
  };

  return { updateInfo, dismissUpdate };
};
