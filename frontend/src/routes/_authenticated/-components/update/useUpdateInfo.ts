import { useEffect, useState } from "react";

import useAuth from "@/hooks/useAuth";
import type { UpdateInfo } from "@/types/auth";

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

const isUpdateInfo = (value: unknown): value is UpdateInfo => {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return false;
  }

  const candidate = value as Record<string, unknown>;
  return (
    typeof candidate.available === "boolean" &&
    typeof candidate.current_version === "string" &&
    (candidate.latest_version === undefined ||
      typeof candidate.latest_version === "string") &&
    (candidate.release_url === undefined ||
      typeof candidate.release_url === "string")
  );
};

interface StoredUpdateInfo {
  malformed: boolean;
  value: UpdateInfo | null;
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
    if (isUpdateInfo(parsed)) return { malformed: false, value: parsed };
  } catch {
    // Fall through and remove malformed data below.
  }

  return { malformed: true, value: null };
};

const loadUpdateInfo = (): UpdateInfo | null => readStoredUpdateInfo().value;

const clearMalformedStoredUpdateInfo = () => {
  if (!readStoredUpdateInfo().malformed) return;

  removeStoredValue(UPDATE_INFO_KEY);
  removeStoredValue(UPDATE_INFO_CHECKED_KEY);
};

export const useUpdateInfo = () => {
  const { isAuthenticated, privileged } = useAuth();
  const [updateInfo, setUpdateInfo] = useState<UpdateInfo | null>(
    loadUpdateInfo,
  );

  useEffect(() => {
    clearMalformedStoredUpdateInfo();

    if (!isAuthenticated || !privileged || hasCheckedForUpdate()) return;

    const controller = new AbortController();
    let active = true;

    const fetchUpdateInfo = async () => {
      try {
        const response = await fetch("/api/update-info", {
          cache: "no-store",
          credentials: "include",
          signal: controller.signal,
        });

        if (!active || hasCheckedForUpdate()) return;

        if (response.status === 204) {
          setUpdateInfo(null);
          removeStoredValue(UPDATE_INFO_KEY);
          setStoredValue(UPDATE_INFO_CHECKED_KEY, "1");
          return;
        }

        if (!response.ok) return;

        const payload: unknown = await response.json();
        if (!active || hasCheckedForUpdate() || !isUpdateInfo(payload)) return;

        setUpdateInfo(payload);
        setStoredValue(UPDATE_INFO_KEY, JSON.stringify(payload));
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
