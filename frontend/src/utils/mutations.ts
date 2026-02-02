import { LinuxIOError } from "@/api/react-query";

/**
 * Centralized error message extraction for mutations
 * Handles LinuxIOError, Error instances, and unknown error types
 */
export const getMutationErrorMessage = (
  error: unknown,
  fallback: string,
): string => {
  if (error instanceof LinuxIOError) {
    return error.message || fallback;
  }
  if (error instanceof Error) {
    return error.message || fallback;
  }
  return fallback;
};
