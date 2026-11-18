import { useSyncExternalStore } from "react";

import type { TrashRestoreRequest } from "@/api";

/** The items of one restore whose names were taken where they go. */
export interface TakenNamesPrompt {
  /** Where each item goes, by its trashed path, in the order it failed. */
  destinations: Readonly<Record<string, string>>;
  id: number;
  /** The restore they failed in, whose choices a second request keeps. */
  request: TrashRestoreRequest;
}

// The questions wait here rather than in the page's state: a restore reports
// when it ends, which may be after the user has left the file browser, and a
// question that lived in the page would be lost with it. The file browser
// asks them, one restore after another in the order they ended, whenever it
// shows; a question stays here until it is answered.
let prompts: readonly TakenNamesPrompt[] = [];
let nextId = 1;
// File browser pages showing now, each of which asks the questions.
let askers = 0;
const listeners = new Set<() => void>();

const emit = () => {
  for (const listener of listeners) listener();
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};

/** Asks what to do with the items of request whose names were taken. */
export function askAboutTakenNames(
  request: TrashRestoreRequest,
  destinations: Record<string, string>,
): void {
  if (Object.keys(destinations).length === 0) return;
  prompts = [...prompts, { destinations, id: nextId++, request }];
  emit();
}

/** Drops an answered question, whatever the answer was. */
export function settleTakenNamesPrompt(id: number): void {
  if (!prompts.some((prompt) => prompt.id === id)) return;
  prompts = prompts.filter((prompt) => prompt.id !== id);
  emit();
}

/**
 * Marks a file browser page that asks the questions while it shows, until
 * the returned function is called.
 */
export function showTakenNamesAsker(): () => void {
  askers += 1;
  return () => {
    askers -= 1;
  };
}

/** Whether a file browser page shows to ask a question now. */
export function takenNamesAskerShows(): boolean {
  return askers > 0;
}

/** The question to ask next, or null when none waits. */
export function nextTakenNamesPrompt(): TakenNamesPrompt | null {
  return prompts[0] ?? null;
}

export function useNextTakenNamesPrompt(): TakenNamesPrompt | null {
  return useSyncExternalStore(
    subscribe,
    nextTakenNamesPrompt,
    nextTakenNamesPrompt,
  );
}
