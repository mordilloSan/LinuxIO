import { useState } from "react";

interface UpdateInfo {
  available: boolean;
  current_version: string;
  latest_version?: string;
  release_url?: string;
}

const loadUpdateInfo = (): UpdateInfo | null => {
  const stored = sessionStorage.getItem("update_info");
  if (stored) {
    try {
      return JSON.parse(stored);
    } catch (error) {
      console.error("Failed to parse update info:", error);
    }
  }
  return null;
};

export const useUpdateInfo = () => {
  const [updateInfo, setUpdateInfo] = useState<UpdateInfo | null>(
    loadUpdateInfo,
  );

  const dismissUpdate = () => {
    setUpdateInfo(null);
    sessionStorage.removeItem("update_info");
  };

  return { updateInfo, dismissUpdate };
};
