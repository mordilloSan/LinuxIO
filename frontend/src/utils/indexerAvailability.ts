const STORAGE_KEY = "indexer_available";

const getStorage = (): Storage | null => {
  if (typeof window === "undefined") return null;
  try {
    return window.sessionStorage;
  } catch {
    return null;
  }
};

export const setIndexerAvailabilityFlag = (available?: boolean | null) => {
  const storage = getStorage();
  if (!storage) return;
  if (available === null || available === undefined) {
    storage.removeItem(STORAGE_KEY);
    return;
  }
  storage.setItem(STORAGE_KEY, available ? "true" : "false");
};

export const clearIndexerAvailabilityFlag = () => {
  const storage = getStorage();
  if (!storage) return;
  storage.removeItem(STORAGE_KEY);
};

export const getIndexerAvailabilityFlag = (): boolean | null => {
  const storage = getStorage();
  if (!storage) return null;
  const value = storage.getItem(STORAGE_KEY);
  if (value === null) return null;
  return value === "true";
};
