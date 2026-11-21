// Client for the webserver's plain HTTP endpoints (backend/webserver/auth and
// backend/webserver/appupdate). Everything else travels over the stream mux.

// The auth endpoints honor VITE_API_URL; the /api/* endpoints are same-origin.
const API_BASE = import.meta.env.VITE_API_URL || "";

interface LoginResponse {
  privileged: boolean;
  success: boolean;
}

type LoginErrorCode =
  | "invalid_request"
  | "session_creation_failed"
  | "too_many_requests"
  | "authentication_failed"
  | "password_expired"
  | "access_denied"
  | "bridge_error"
  | "internal_error"
  | "login_failed";

interface LoginErrorResponse {
  code?: LoginErrorCode;
  error?: string;
}

const loginErrorMessage = (
  code?: LoginErrorCode,
  fallback?: string,
): string => {
  switch (code) {
    case "invalid_request":
      return "The sign-in request was invalid. Refresh the page and try again.";
    case "session_creation_failed":
      return "LinuxIO could not prepare your session. Please try again.";
    case "too_many_requests":
      return "Too many sign-in attempts at once. Wait a moment and try again.";
    case "authentication_failed":
      return "Incorrect username or password.";
    case "password_expired":
      return "Your password has expired. Change it in SSH or on the system console, then try again.";
    case "access_denied":
      return "This account is not allowed to sign in from the web interface.";
    case "bridge_error":
      return "LinuxIO authenticated you, but could not start the session bridge. Please try again.";
    case "internal_error":
      return "LinuxIO could not complete sign-in. Please try again.";
    default:
      return fallback || "Login failed";
  }
};

/** Rejects with a user-presentable message when the credentials are refused. */
export async function login(
  username: string,
  password: string,
): Promise<LoginResponse> {
  const res = await fetch(`${API_BASE}/auth/login`, {
    method: "POST",
    credentials: "include",
    cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password }),
  });
  if (!res.ok) {
    const err = (await res.json().catch(() => ({}))) as LoginErrorResponse;
    throw new Error(loginErrorMessage(err.code, err.error));
  }
  return res.json();
}

export async function logout(): Promise<void> {
  await fetch(`${API_BASE}/auth/logout`, {
    method: "GET",
    credentials: "include",
    cache: "no-store",
  });
}

/** True when the webserver answers `/api/version`; rejects on network failure. */
export async function webserverResponds(signal?: AbortSignal) {
  const response = await fetch("/api/version", {
    method: "GET",
    cache: "no-store",
    signal,
  });
  return response.ok;
}
