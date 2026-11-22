const SIGNIN_NOTICE_KEY = "signin_notice";

export type SigninNotice = "expired";

/** User-facing copy for each one-shot sign-in notice. */
export const SIGNIN_NOTICE_MESSAGES: Record<SigninNotice, string> = {
  expired: "Your session expired. Please sign in again.",
};

/**
 * Records a one-shot notice to surface on the sign-in screen after an
 * involuntary redirect (e.g. an expired session). Survives the hard reload in
 * `redirectToSignIn`; deliberate sign-out does not set one.
 */
export function setSigninNotice(notice: SigninNotice): void {
  try {
    sessionStorage.setItem(SIGNIN_NOTICE_KEY, notice);
  } catch {
    /* ignore */
  }
}

/** Reads a valid pending notice without mutating storage. */
export function readSigninNotice(): SigninNotice | null {
  try {
    const value = sessionStorage.getItem(SIGNIN_NOTICE_KEY);
    return value === "expired" ? value : null;
  } catch {
    return null;
  }
}

/** Clears any pending or unrecognized sign-in notice. */
export function clearSigninNotice(): void {
  try {
    sessionStorage.removeItem(SIGNIN_NOTICE_KEY);
  } catch {
    /* ignore */
  }
}
