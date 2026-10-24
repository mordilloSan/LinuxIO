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
