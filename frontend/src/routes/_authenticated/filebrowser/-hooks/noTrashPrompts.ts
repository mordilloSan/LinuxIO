import { useSyncExternalStore } from "react";

/** The items of one move to Trash whose disk cannot hold a trash. */
export interface NoTrashPrompt {
  id: number;
  paths: readonly string[];
}

// The questions wait here rather than in a page's state: a move to Trash
// reports when it ends, which may be after the user has moved to another
// folder, into the Trash, or off the file browser, and a question that lived
// in the page it started from would be lost with it. Each job is asked about
// on its own, in the order they ended, so a dialog never changes what it
// asks while the user is reading it.
let prompts: readonly NoTrashPrompt[] = [];
let nextId = 1;
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

/** Asks whether to delete these items for good instead. */
export function askToDeleteForGood(paths: readonly string[]): void {
  if (paths.length === 0) return;
  prompts = [...prompts, { id: nextId++, paths: [...paths] }];
  emit();
}

/** Drops an answered question, whatever the answer was. */
export function settleNoTrashPrompt(id: number): void {
  if (!prompts.some((prompt) => prompt.id === id)) return;
  prompts = prompts.filter((prompt) => prompt.id !== id);
  emit();
}

/** The question to ask next, or null when none waits. */
export function nextNoTrashPrompt(): NoTrashPrompt | null {
  return prompts[0] ?? null;
}

export function useNextNoTrashPrompt(): NoTrashPrompt | null {
  return useSyncExternalStore(subscribe, nextNoTrashPrompt, nextNoTrashPrompt);
}
