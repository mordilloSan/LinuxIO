import { describe, expect, it, vi } from "vitest";

import { withPromiseCleanup } from "@/utils/withPromiseCleanup";

describe("withPromiseCleanup", () => {
  it("runs cleanup while preserving fulfillment and rejection", async () => {
    const cleanup = vi.fn();
    const error = new Error("operation failed");

    await expect(
      withPromiseCleanup(Promise.resolve("result"), cleanup),
    ).resolves.toBe("result");
    await expect(
      withPromiseCleanup(Promise.reject(error), cleanup),
    ).rejects.toBe(error);
    expect(cleanup).toHaveBeenCalledTimes(2);
  });

  it("lets a cleanup error replace the operation result", async () => {
    const cleanupError = new Error("cleanup failed");
    const cleanup = () => {
      throw cleanupError;
    };

    await expect(
      withPromiseCleanup(Promise.resolve("result"), cleanup),
    ).rejects.toBe(cleanupError);
    await expect(
      withPromiseCleanup(
        Promise.reject(new Error("operation failed")),
        cleanup,
      ),
    ).rejects.toBe(cleanupError);
  });
});
