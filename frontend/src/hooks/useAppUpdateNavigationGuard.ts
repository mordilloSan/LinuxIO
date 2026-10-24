import { useBlocker } from "@tanstack/react-router";

export const useAppUpdateNavigationGuard = (isUpdating: boolean) => {
  useBlocker({
    disabled: !isUpdating,
    enableBeforeUnload: isUpdating,
    shouldBlockFn: () => isUpdating,
  });
};
