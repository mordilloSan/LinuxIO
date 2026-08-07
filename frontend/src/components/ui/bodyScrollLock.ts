let lockCount = 0;
let originalOverflow: string | null = null;

/**
 * Acquires the shared body scroll lock and returns an idempotent release
 * callback. The inline overflow value is captured once for the first owner
 * and restored only after the last owner releases the lock.
 */
export function acquireBodyScrollLock(): () => void {
  if (lockCount === 0) {
    originalOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
  }
  lockCount += 1;

  let released = false;
  return () => {
    if (released) {
      return;
    }
    released = true;
    lockCount -= 1;
    if (lockCount === 0) {
      document.body.style.overflow = originalOverflow ?? "";
      originalOverflow = null;
    }
  };
}
