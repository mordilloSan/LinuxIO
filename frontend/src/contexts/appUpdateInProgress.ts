// The single record of "a LinuxIO self-update is in progress". The provider
// publishes it; React consumers read it with useAppUpdateInProgress, and the
// router (which mounts above the provider) reads it through
// isAppUpdateInProgress / subscribeAppUpdateInProgress.

let publisher: object | null = null;
let inProgress = false;
const listeners = new Set<() => void>();

export const isAppUpdateInProgress = () => inProgress;

export const subscribeAppUpdateInProgress = (
  listener: () => void,
): (() => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};

const setInProgress = (value: boolean) => {
  if (inProgress === value) return;
  inProgress = value;
  listeners.forEach((listener) => listener());
};

/** Returns a release that clears the flag, unless a newer publisher took over. */
export const publishAppUpdateInProgress = (owner: object, value: boolean) => {
  publisher = owner;
  setInProgress(value);

  return () => {
    if (publisher !== owner) return;
    publisher = null;
    setInProgress(false);
  };
};
