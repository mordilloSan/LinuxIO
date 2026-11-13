const FRESH_SIGN_IN_KEY = "fresh_sign_in";

/**
 * Marks that a session has just started, so the shell can say once what
 * happened while the user was away. A page reload in the same session finds
 * the mark already taken. Storage that refuses the write means no notice.
 */
export function markFreshSignIn(): void {
  try {
    sessionStorage.setItem(FRESH_SIGN_IN_KEY, "1");
  } catch {
    /* ignore */
  }
}

/** Whether a sign-in mark is waiting, without taking it. */
export function hasFreshSignIn(): boolean {
  try {
    return sessionStorage.getItem(FRESH_SIGN_IN_KEY) !== null;
  } catch {
    return false;
  }
}

/** Drops a mark nobody took, when the session ends. */
export function clearFreshSignIn(): void {
  try {
    sessionStorage.removeItem(FRESH_SIGN_IN_KEY);
  } catch {
    /* ignore */
  }
}

/** Takes the sign-in mark: true for the first caller after a sign-in only. */
export function takeFreshSignIn(): boolean {
  try {
    if (sessionStorage.getItem(FRESH_SIGN_IN_KEY) === null) return false;
    sessionStorage.removeItem(FRESH_SIGN_IN_KEY);
    return true;
  } catch {
    return false;
  }
}
