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

export interface AppUpdateInfo {
  available: boolean;
  current_version: string;
  latest_version?: string;
  release_url?: string;
}

export interface AppUpdateStatus {
  exit_code?: number;
  finished_at?: number;
  id?: string;
  message?: string;
  started_at?: number;
  status: "unknown" | "running" | "ok" | "error";
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

type ComponentVersions = Record<string, unknown>;

/** Component versions keyed by name, or null when the server is unreachable. */
export async function fetchComponentVersions(): Promise<ComponentVersions | null> {
  try {
    const response = await fetch("/api/version", {
      method: "GET",
      cache: "no-store",
    });
    if (!response.ok) return null;
    return (await response.json()) as ComponentVersions;
  } catch {
    return null;
  }
}

/** Progress of one self-update run, or null when it cannot be read. */
export async function fetchAppUpdateStatus(
  runId: string,
): Promise<AppUpdateStatus | null> {
  try {
    const response = await fetch(
      `/api/update-status?id=${encodeURIComponent(runId)}`,
      { method: "GET", cache: "no-store" },
    );
    if (!response.ok) return null;
    return (await response.json()) as AppUpdateStatus;
  } catch {
    return null;
  }
}

export const isAppUpdateInfo = (value: unknown): value is AppUpdateInfo => {
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

/**
 * Latest-release lookup. Resolves `null` when the server has nothing to report
 * (204), `undefined` when the answer is unusable and should be retried later,
 * and rejects on network failure.
 */
export async function fetchAppUpdateInfo(
  signal?: AbortSignal,
): Promise<AppUpdateInfo | null | undefined> {
  const response = await fetch("/api/update-info", {
    cache: "no-store",
    credentials: "include",
    signal,
  });
  if (response.status === 204) return null;
  if (!response.ok) return undefined;
  const payload: unknown = await response.json();
  return isAppUpdateInfo(payload) ? payload : undefined;
}
