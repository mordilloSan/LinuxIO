import { useCallback, useRef, useState } from "react";

import { call, linuxio } from "@/api";

export type ConflictDecision = "overwrite" | "skip" | "keep";

interface FileConflictItem {
  path: string; // absolute destination path
  name: string; // display name, relative to the destination
  isDir: boolean;
  existing: { size: number; modified: string }; // modified: RFC 3339 or ""
  incoming?: { size?: number; modified?: string };
}

export interface ConflictPrompt {
  conflicts: FileConflictItem[];
  destination: string;
}

interface ConflictResolution<T> {
  // Everything that proceeds, including the keep-both items.
  kept: T[];
  overwrite: boolean;
  // The subset of kept that lands under a unique "name (copy)" instead.
  keepBoth: T[];
}

export type ResolveCollisionsFn = <T>(
  items: T[],
  getDestPath: (item: T) => string,
  destination: string,
  describeIncoming?: (item: T) => FileConflictItem["incoming"],
) => Promise<ConflictResolution<T> | null>;

// Thrown by mutations when the user cancels the conflict prompt: callers treat
// it as "operation aborted by choice", not an error worth toasting.
export const CONFLICT_PROMPT_CANCELLED = new Error("conflict prompt cancelled");

const displayName = (destPath: string, destination: string): string => {
  const prefix = destination.endsWith("/") ? destination : `${destination}/`;
  return destPath.startsWith(prefix) ? destPath.slice(prefix.length) : destPath;
};

/**
 * Owns the collision-resolution flow shared by paste (copy/move) and uploads:
 * pre-check the destinations server-side, and when collisions exist, prompt
 * the user for a per-item Overwrite/Skip/Keep both decision before any bytes move.
 * Transfers never overwrite unless the user explicitly chose to.
 */
export function useFileConflictResolution() {
  const [conflictPrompt, setConflictPrompt] = useState<ConflictPrompt | null>(
    null,
  );
  const resolverRef = useRef<
    ((decisions: Record<string, ConflictDecision> | null) => void) | null
  >(null);

  const settle = useCallback(
    (decisions: Record<string, ConflictDecision> | null) => {
      const resolver = resolverRef.current;
      resolverRef.current = null;
      setConflictPrompt(null);
      resolver?.(decisions);
    },
    [],
  );

  const applyConflictDecisions = useCallback(
    (decisions: Record<string, ConflictDecision>) => settle(decisions),
    [settle],
  );

  const cancelConflictPrompt = useCallback(() => settle(null), [settle]);

  const requestDecisions = useCallback(
    (conflicts: FileConflictItem[], destination: string) =>
      new Promise<Record<string, ConflictDecision> | null>((resolve) => {
        // A new prompt supersedes an unanswered one (treated as cancelled).
        resolverRef.current?.(null);
        resolverRef.current = resolve;
        setConflictPrompt({ conflicts, destination });
      }),
    [],
  );

  const resolveCollisions = useCallback<ResolveCollisionsFn>(
    async (items, getDestPath, destination, describeIncoming) => {
      if (!items.length) {
        return { kept: items, overwrite: false, keepBoth: [] };
      }

      let existingByPath: Map<
        string,
        { isDir: boolean; size: number; modified: string }
      >;
      try {
        // Collision pre-checks must reflect the live filesystem — never cache.
        const response = await call(linuxio.filebrowser.exists_batch.route, {
          paths: items.map(getDestPath),
        });
        existingByPath = new Map(
          (response.existing ?? []).map((entry) => [
            entry.path,
            { isDir: entry.isDir, size: entry.size, modified: entry.modified },
          ]),
        );
      } catch {
        // Pre-check unavailable: proceed without overwriting; any collision
        // surfaces as a per-item failure in the task result instead.
        return { kept: items, overwrite: false, keepBoth: [] };
      }
      if (!existingByPath.size) {
        return { kept: items, overwrite: false, keepBoth: [] };
      }

      const conflicts: FileConflictItem[] = [];
      const seen = new Set<string>();
      for (const item of items) {
        const destPath = getDestPath(item);
        const hit = existingByPath.get(destPath);
        if (hit && !seen.has(destPath)) {
          seen.add(destPath);
          conflicts.push({
            path: destPath,
            name: displayName(destPath, destination),
            isDir: hit.isDir,
            existing: { size: hit.size, modified: hit.modified },
            incoming: describeIncoming?.(item),
          });
        }
      }
      if (!conflicts.length) {
        return { kept: items, overwrite: false, keepBoth: [] };
      }

      const decisions = await requestDecisions(conflicts, destination);
      if (!decisions) {
        return null;
      }

      const kept = items.filter((item) => {
        const decision = decisions[getDestPath(item)];
        return decision !== "skip";
      });
      const overwrite = Object.values(decisions).includes("overwrite");
      const keepBoth = kept.filter(
        (item) => decisions[getDestPath(item)] === "keep",
      );
      return { kept, overwrite, keepBoth };
    },
    [requestDecisions],
  );

  return {
    applyConflictDecisions,
    cancelConflictPrompt,
    conflictPrompt,
    resolveCollisions,
  };
}
