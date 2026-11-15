/**
 * LinuxIOError - structured error with code
 */
export class LinuxIOError extends Error {
  code?: string | number;

  constructor(message: string, code?: string | number) {
    super(message);
    this.code = code;
    this.name = "LinuxIOError";
  }
}

export type ConnectionLossCode = "connection_unavailable" | "outcome_unknown";

export function isConnectionLossError(error: unknown): boolean {
  return (
    error instanceof LinuxIOError &&
    (error.code === "connection_unavailable" ||
      error.code === "outcome_unknown")
  );
}

/** What the host reports for a job its stop cut short; see `jobshost`. */
const INTERRUPTED_JOB_MESSAGE = "job was interrupted";

/** What a failed request proves about whether the host acted on it. */
export type RequestOutcome = "not_sent" | "failed" | "unknown";

/**
 * Classifies the error of a request whose route may take the host down:
 *
 * - `not_sent`: `connection_unavailable`, no stream was opened.
 * - `failed`: the host answered that it did not do it: a numeric 400 to 599
 *   code. The bridge gives a handler's plain error a 500 (logind denying a
 *   reboot, NetworkManager rejecting a setting), and a refusal such as
 *   `update_pending` a 409. The exceptions are the answers of a host that is
 *   going away, below.
 * - `unknown`: nothing says whether the host acted. A lost connection
 *   (`outcome_unknown`, `connection_closed`), a client `timeout` or any other
 *   non-numeric code; the 409 "job was interrupted", which a stop of the job
 *   host answers for a job that may have run; the 503 "job host is stopping" /
 *   "job host unavailable" and the 504 of a host or bridge that is going away;
 *   a code outside 400 to 599; and an error that is not a LinuxIOError.
 */
export function classifyRequestOutcome(error: unknown): RequestOutcome {
  if (!(error instanceof LinuxIOError)) return "unknown";
  const { code } = error;
  if (code === "connection_unavailable") return "not_sent";
  if (typeof code !== "number" || code < 400 || code > 599) return "unknown";
  if (code === 503 || code === 504) return "unknown";
  if (code === 409 && error.message === INTERRUPTED_JOB_MESSAGE) {
    return "unknown";
  }
  return "failed";
}

/**
 * Centralized error message extraction for mutations
 * Handles LinuxIOError, Error instances, and unknown error types
 */
export const getMutationErrorMessage = (
  error: unknown,
  fallback: string,
): string => {
  if (error instanceof Error) {
    return error.message || fallback;
  }
  return fallback;
};
