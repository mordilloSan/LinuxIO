import type { Update } from "@/api";

export const PACKAGEKIT_INFO_BLOCKED = 9;
export const PACKAGEKIT_INFO_UNAVAILABLE = 25;

export function isDeferredUpdate(update: Pick<Update, "info_enum">): boolean {
  return (
    update.info_enum === PACKAGEKIT_INFO_BLOCKED ||
    update.info_enum === PACKAGEKIT_INFO_UNAVAILABLE
  );
}

export function partitionUpdatesByAvailability(updates: Update[]): {
  actionable: Update[];
  deferred: Update[];
} {
  const actionable: Update[] = [];
  const deferred: Update[] = [];

  for (const update of updates) {
    if (isDeferredUpdate(update)) {
      deferred.push(update);
    } else {
      actionable.push(update);
    }
  }

  return { actionable, deferred };
}
