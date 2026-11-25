import { createContext, type ReactNode, use } from "react";
import { createPortal } from "react-dom";

/**
 * The column the dock's cards stack in, beside its tabs. undefined: no dock
 * around (a card rendered alone); null: the dock has not mounted it yet.
 */
export const DockCardsHost = createContext<HTMLElement | null | undefined>(
  undefined,
);

/**
 * Places a card in the dock's column of cards, so the open cards stack under
 * one another instead of lying over each other at the height of their tab.
 */
export default function DockCard({ children }: { children: ReactNode }) {
  const host = use(DockCardsHost);
  if (host === undefined) return children;
  return host ? createPortal(children, host) : null;
}
