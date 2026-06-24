import { describe, expect, it, vi } from "vitest";

import { lazyWithPreload } from "@/routing/lazyWithPreload";

function TestComponent() {
  return <div>loaded</div>;
}

describe("lazyWithPreload", () => {
  it("dedupes concurrent preload calls", async () => {
    let resolve!: (value: { default: typeof TestComponent }) => void;
    const importer = vi.fn(
      () =>
        new Promise<{ default: typeof TestComponent }>((r) => {
          resolve = r;
        }),
    );
    const Component = lazyWithPreload(importer);

    const first = Component.preload();
    const second = Component.preload();

    expect(first).toBe(second);
    expect(importer).toHaveBeenCalledTimes(1);

    resolve({ default: TestComponent });
    await expect(first).resolves.toEqual({ default: TestComponent });
  });

  it("reuses successful preload results", async () => {
    const importer = vi.fn(async () => ({ default: TestComponent }));
    const Component = lazyWithPreload(importer);

    await Component.preload();
    await Component.preload();

    expect(importer).toHaveBeenCalledTimes(1);
  });

  it("resets the cached promise after preload failures", async () => {
    const importer = vi
      .fn<() => Promise<{ default: typeof TestComponent }>>()
      .mockRejectedValueOnce(new Error("chunk failed"))
      .mockResolvedValueOnce({ default: TestComponent });
    const Component = lazyWithPreload(importer);

    await expect(Component.preload()).rejects.toThrow("chunk failed");
    await expect(Component.preload()).resolves.toEqual({
      default: TestComponent,
    });

    expect(importer).toHaveBeenCalledTimes(2);
  });
});
