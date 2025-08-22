// src/utils/getErrorMessage.ts
import { AxiosError } from "axios";

export function getErrorMessage(error: unknown): string {
  if (error instanceof AxiosError) {
    return (
      (error.response?.data as any)?.message ||
      error.message ||
      "An unexpected error occurred."
    );
  }
  if (error instanceof Error) return error.message;
  return String(error);
}
