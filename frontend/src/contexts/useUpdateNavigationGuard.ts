import { useBlocker } from "@tanstack/react-router";

export const useUpdateNavigationGuard = (isUpdating: boolean) => {
  useBlocker({
    disabled: !isUpdating,
    enableBeforeUnload: isUpdating,
    shouldBlockFn: () => isUpdating,
  });
};
