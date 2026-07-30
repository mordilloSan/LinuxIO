import { expect, it } from "vitest";

import { renderHook } from "@/test/render";

import { useMountedGuard } from "./useMountedGuard";

it("turns false after unmount so delayed work can avoid dispatching", () => {
  const { result, unmount } = renderHook(() => useMountedGuard());
  const isMounted = result.current;

  expect(isMounted()).toBe(true);
  unmount();
  expect(isMounted()).toBe(false);
});
