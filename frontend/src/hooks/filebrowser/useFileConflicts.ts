import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useRef, useState } from "react";

import { CACHE_TTL_MS, linuxio } from "@/api";

export type ConflictDecision = "overwrite" | "skip";

export interface FileConflictItem {
  path: string; // absolute destination path
  name: string; // display name, relative to the destination
  isDir: boolean;
}

export interface ConflictPrompt {
  conflicts: FileConflictItem[];
  destination: string;
}

export interface ConflictResolution<T> {
  kept: T[];
  overwrite: boolean;
}

export type ResolveCollisionsFn = <T>(
  items: T[],
  getDestPath: (item: T) => string,
  destination: string,
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
 * the user for a per-item Overwrite/Skip decision before any bytes move.
 * Transfers never overwrite unless the user explicitly chose to.
 */
export function useFileConflictResolution() {
  const queryClient = useQueryClient();
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
    async (items, getDestPath, destination) => {
      if (!items.length) {
        return { kept: items, overwrite: false };
      }

      let existingByPath: Map<string, { isDir: boolean }>;
      try {
        // Collision pre-checks must reflect the live filesystem — never cache.
        const response = await queryClient.fetchQuery(
          linuxio.filebrowser.exists_batch.queryOptions(
            items.map(getDestPath),
            { staleTime: CACHE_TTL_MS.NONE, gcTime: CACHE_TTL_MS.NONE },
          ),
        );
        existingByPath = new Map(
          (response.existing ?? []).map((entry) => [
            entry.path,
            { isDir: entry.isDir },
          ]),
        );
      } catch {
        // Pre-check unavailable: proceed without overwriting; any collision
        // surfaces as a per-item failure in the job result instead.
        return { kept: items, overwrite: false };
      }
      if (!existingByPath.size) {
        return { kept: items, overwrite: false };
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
          });
        }
      }
      if (!conflicts.length) {
        return { kept: items, overwrite: false };
      }

      const decisions = await requestDecisions(conflicts, destination);
      if (!decisions) {
        return null;
      }

      const kept = items.filter((item) => {
        const decision = decisions[getDestPath(item)];
        return decision === undefined || decision === "overwrite";
      });
      const overwrite = Object.values(decisions).includes("overwrite");
      return { kept, overwrite };
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
