import { describe, expect, it, vi } from "vitest";

const apiMocks = vi.hoisted(() => ({
  exists_batch: vi.fn(),
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      filebrowser: {
        ...actual.linuxio.filebrowser,
        exists_batch: apiMocks.exists_batch,
      },
    },
  };
});

const { useFileConflictResolution } =
  await import("@/hooks/filebrowser/useFileConflicts");
const { act, renderHook } = await import("@/test/render");

const items = ["/src/a.txt", "/src/sub/b.txt"];
const getDestPath = (source: string) =>
  `/dest/${source.split("/").slice(2).join("/")}`;

describe("useFileConflictResolution", () => {
  it("passes items through untouched when nothing exists at the destination", async () => {
    apiMocks.exists_batch.mockResolvedValue({ existing: [] });
    const { result } = renderHook(() => useFileConflictResolution());

    let resolution;
    await act(async () => {
      resolution = await result.current.resolveCollisions(
        items,
        getDestPath,
        "/dest",
      );
    });

    expect(apiMocks.exists_batch).toHaveBeenCalledWith([
      "/dest/a.txt",
      "/dest/sub/b.txt",
    ]);
    expect(resolution).toEqual({ kept: items, overwrite: false });
    expect(result.current.conflictPrompt).toBeNull();
  });

  it("prompts on collisions and applies per-item decisions", async () => {
    apiMocks.exists_batch.mockResolvedValue({
      existing: [{ isDir: false, path: "/dest/a.txt" }],
    });
    const { result } = renderHook(() => useFileConflictResolution());

    let resolutionPromise!: ReturnType<
      typeof result.current.resolveCollisions<string>
    >;
    act(() => {
      resolutionPromise = result.current.resolveCollisions(
        items,
        getDestPath,
        "/dest",
      );
    });
    await act(async () => {}); // let the exists_batch pre-check settle

    expect(result.current.conflictPrompt).toEqual({
      conflicts: [{ isDir: false, name: "a.txt", path: "/dest/a.txt" }],
      destination: "/dest",
    });

    act(() => {
      result.current.applyConflictDecisions({ "/dest/a.txt": "overwrite" });
    });

    await expect(resolutionPromise).resolves.toEqual({
      kept: items,
      overwrite: true,
    });
    expect(result.current.conflictPrompt).toBeNull();
  });

  it("drops skipped items and reports no overwrite when everything is skipped", async () => {
    apiMocks.exists_batch.mockResolvedValue({
      existing: [{ isDir: false, path: "/dest/a.txt" }],
    });
    const { result } = renderHook(() => useFileConflictResolution());

    let resolutionPromise!: ReturnType<
      typeof result.current.resolveCollisions<string>
    >;
    act(() => {
      resolutionPromise = result.current.resolveCollisions(
        items,
        getDestPath,
        "/dest",
      );
    });
    await act(async () => {});

    act(() => {
      result.current.applyConflictDecisions({ "/dest/a.txt": "skip" });
    });

    await expect(resolutionPromise).resolves.toEqual({
      kept: ["/src/sub/b.txt"],
      overwrite: false,
    });
  });

  it("resolves null when the prompt is cancelled", async () => {
    apiMocks.exists_batch.mockResolvedValue({
      existing: [{ isDir: false, path: "/dest/a.txt" }],
    });
    const { result } = renderHook(() => useFileConflictResolution());

    let resolutionPromise!: ReturnType<
      typeof result.current.resolveCollisions<string>
    >;
    act(() => {
      resolutionPromise = result.current.resolveCollisions(
        items,
        getDestPath,
        "/dest",
      );
    });
    await act(async () => {});

    act(() => {
      result.current.cancelConflictPrompt();
    });

    await expect(resolutionPromise).resolves.toBeNull();
    expect(result.current.conflictPrompt).toBeNull();
  });

  it("proceeds without overwriting when the pre-check itself fails", async () => {
    apiMocks.exists_batch.mockRejectedValue(new Error("offline"));
    const { result } = renderHook(() => useFileConflictResolution());

    let resolution;
    await act(async () => {
      resolution = await result.current.resolveCollisions(
        items,
        getDestPath,
        "/dest",
      );
    });

    expect(resolution).toEqual({ kept: items, overwrite: false });
  });
});
