import { useEffect, useState } from "react";

import {
  type AppUpdateInfo,
  fetchAppUpdateInfo,
  isAppUpdateInfo,
} from "@/api/webserver";
import useAuth from "@/hooks/useAuth";

const UPDATE_INFO_KEY = "update_info";
const UPDATE_INFO_CHECKED_KEY = "update_info_checked";

const removeStoredValue = (key: string) => {
  try {
    sessionStorage.removeItem(key);
  } catch {
    // Storage may be unavailable (for example, in a privacy-restricted tab).
  }
};

const setStoredValue = (key: string, value: string) => {
  try {
    sessionStorage.setItem(key, value);
  } catch {
    // Storage may be unavailable (for example, in a privacy-restricted tab).
  }
};

const hasCheckedForUpdate = () => {
  try {
    return sessionStorage.getItem(UPDATE_INFO_CHECKED_KEY) !== null;
  } catch {
    return false;
  }
};

interface StoredUpdateInfo {
  malformed: boolean;
  value: AppUpdateInfo | null;
}

const readStoredUpdateInfo = (): StoredUpdateInfo => {
  let stored: string | null;
  try {
    stored = sessionStorage.getItem(UPDATE_INFO_KEY);
  } catch {
    return { malformed: false, value: null };
  }

  if (!stored) return { malformed: false, value: null };

  try {
    const parsed: unknown = JSON.parse(stored);
    if (isAppUpdateInfo(parsed)) return { malformed: false, value: parsed };
  } catch {
    // Fall through and remove malformed data below.
  }

  return { malformed: true, value: null };
};

const loadUpdateInfo = (): AppUpdateInfo | null => readStoredUpdateInfo().value;

const clearMalformedStoredUpdateInfo = () => {
  if (!readStoredUpdateInfo().malformed) return;

  removeStoredValue(UPDATE_INFO_KEY);
  removeStoredValue(UPDATE_INFO_CHECKED_KEY);
};

export const useAppUpdateInfo = () => {
  const { isAuthenticated, privileged } = useAuth();
  const [updateInfo, setUpdateInfo] = useState<AppUpdateInfo | null>(
    loadUpdateInfo,
  );

  useEffect(() => {
    clearMalformedStoredUpdateInfo();

    if (!isAuthenticated || !privileged || hasCheckedForUpdate()) return;

    const controller = new AbortController();
    let active = true;

    const fetchUpdateInfo = async () => {
      try {
        const info = await fetchAppUpdateInfo(controller.signal);

        if (!active || hasCheckedForUpdate() || info === undefined) return;

        if (info === null) {
          setUpdateInfo(null);
          removeStoredValue(UPDATE_INFO_KEY);
          setStoredValue(UPDATE_INFO_CHECKED_KEY, "1");
          return;
        }

        setUpdateInfo(info);
        setStoredValue(UPDATE_INFO_KEY, JSON.stringify(info));
        setStoredValue(UPDATE_INFO_CHECKED_KEY, "1");
      } catch {
        // Transient failures remain unfetched and can be retried on a later mount.
      }
    };

    void fetchUpdateInfo();

    return () => {
      active = false;
      controller.abort();
    };
  }, [isAuthenticated, privileged]);

  const dismissUpdate = () => {
    setUpdateInfo(null);
    removeStoredValue(UPDATE_INFO_KEY);
    setStoredValue(UPDATE_INFO_CHECKED_KEY, "1");
  };

  return { updateInfo, dismissUpdate };
};
