import type { MouseEvent } from "react";

import { clickTargetsRowBody } from "@/components/tables/rowInteraction";

/** Gives the card body the pointer cursor its click target implies. */
export const CARD_BODY_TOGGLE_CLASS = "card-body-toggle";

/**
 * A settings card whose whole body is a hit target for the one control it
 * carries — the way a <label> extends a checkbox's hit box across its own text.
 *
 * Deliberately not a control: no role, no tabIndex, no key handler. The switch
 * or pill group inside stays the single tab stop and the single thing a screen
 * reader announces, so this adds a pointer convenience without adding a second
 * way to say the same thing. Only give a card body a toggle if it lifts —
 * without the lift there is nothing telling a pointer the body is clickable.
 *
 * Clicks that land on a control inside the card, or that end a text selection,
 * belong to the control or to the selection. clickTargetsRowBody already draws
 * that line for table rows, and a settings card is the same shape.
 */
const bodyClickProps = (act: () => void) => ({
  className: CARD_BODY_TOGGLE_CLASS,
  onClick: (event: MouseEvent<HTMLElement>) => {
    if (!clickTargetsRowBody(event.target)) return;
    act();
  },
});

/**
 * For a card carrying one switch. Returns nothing while disabled, so a card
 * that cannot act does not offer a cursor that says it can.
 *
 * Callers with a className of their own must join CARD_BODY_TOGGLE_CLASS
 * themselves rather than spreading this over it.
 */
export function cardBodyToggleProps({
  checked,
  disabled,
  onChange,
}: {
  checked: boolean;
  disabled?: boolean;
  onChange: (next: boolean) => void;
}) {
  if (disabled) return {};
  return bodyClickProps(() => onChange(!checked));
}

/**
 * For a card carrying a two-option pill group (light/dark, sidebar/dock): the
 * body advances to the next value, which for a pair is a flip. A group narrowed
 * to a single option has nothing to advance to and gets no click target.
 */
export function cardBodyCycleProps<T>({
  disabled,
  onChange,
  value,
  values,
}: {
  disabled?: boolean;
  onChange: (next: T) => void;
  value: T;
  values: readonly T[];
}) {
  if (disabled || values.length < 2) return {};
  return bodyClickProps(() => {
    const index = values.indexOf(value);
    onChange(values[(index + 1) % values.length]);
  });
}
