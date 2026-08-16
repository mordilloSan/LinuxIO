import { afterEach, beforeEach, describe, expect, it } from "vitest";

import {
  getInputModality,
  installInputModalityTracking,
  POINTER_ACTIVE_ATTRIBUTE,
  POINTER_FOCUS_ATTRIBUTE,
} from "@/utils/inputModality";

describe("inputModality", () => {
  let uninstall: () => void;
  let button: HTMLButtonElement;
  let input: HTMLInputElement;

  beforeEach(() => {
    button = document.createElement("button");
    input = document.createElement("input");
    document.body.append(button, input);
    uninstall = installInputModalityTracking();
  });

  afterEach(() => {
    uninstall();
    button.remove();
    input.remove();
  });

  const pointerDown = (target: Element) =>
    target.dispatchEvent(new MouseEvent("pointerdown", { bubbles: true }));
  const keyDown = (key: string) =>
    document.dispatchEvent(
      new KeyboardEvent("keydown", { bubbles: true, key }),
    );

  it("defaults to keyboard, the modality that shows rings", () => {
    expect(getInputModality()).toBe("keyboard");
  });

  it("marks focus a pointer press took", () => {
    pointerDown(button);
    button.focus();

    expect(getInputModality()).toBe("pointer");
    expect(button).toHaveAttribute(POINTER_FOCUS_ATTRIBUTE);
  });

  it("requires fresh pointer activity after the window deactivates", () => {
    pointerDown(button);
    expect(document.documentElement).toHaveAttribute(POINTER_ACTIVE_ATTRIBUTE);

    window.dispatchEvent(new Event("blur"));
    expect(document.documentElement).not.toHaveAttribute(
      POINTER_ACTIVE_ATTRIBUTE,
    );

    // Regaining focus alone must not revive hover affordances. The browser can
    // restore :hover here even though the app receives no pointer event.
    window.dispatchEvent(new Event("focus"));
    expect(document.documentElement).not.toHaveAttribute(
      POINTER_ACTIVE_ATTRIBUTE,
    );

    document.dispatchEvent(new MouseEvent("pointermove", { bubbles: true }));
    expect(document.documentElement).toHaveAttribute(POINTER_ACTIVE_ATTRIBUTE);
  });

  it("leaves keyboard-taken focus unmarked", () => {
    keyDown("Tab");
    button.focus();

    expect(getInputModality()).toBe("keyboard");
    expect(button).not.toHaveAttribute(POINTER_FOCUS_ATTRIBUTE);
  });

  it("keeps the mark through the keys that flip :focus-visible", () => {
    pointerDown(button);
    button.focus();

    // The bug in one line: Escape is what the tables bind, and it is what turns
    // a clicked chevron into a ringed one. Arrow keys do it too.
    keyDown("Escape");
    expect(button).toHaveAttribute(POINTER_FOCUS_ATTRIBUTE);

    keyDown("ArrowDown");
    expect(button).toHaveAttribute(POINTER_FOCUS_ATTRIBUTE);
  });

  it("gives the ring back once the keyboard activates the control", () => {
    pointerDown(button);
    button.focus();
    keyDown("Enter");

    expect(button).not.toHaveAttribute(POINTER_FOCUS_ATTRIBUTE);
  });

  it("drops the mark when focus leaves", () => {
    pointerDown(button);
    button.focus();
    input.focus();

    expect(button).not.toHaveAttribute(POINTER_FOCUS_ATTRIBUTE);
  });

  it("marks a focus moved programmatically during a pointer interaction", () => {
    // A dialog restoring focus to the trigger it was opened from lands here.
    pointerDown(button);
    input.focus();

    expect(input).toHaveAttribute(POINTER_FOCUS_ATTRIBUTE);
  });

  it("is idempotent and unbinds cleanly", () => {
    const second = installInputModalityTracking();
    second();

    pointerDown(button);
    button.focus();

    expect(getInputModality()).toBe("keyboard");
    expect(button).not.toHaveAttribute(POINTER_FOCUS_ATTRIBUTE);
    expect(document.documentElement).not.toHaveAttribute(
      POINTER_ACTIVE_ATTRIBUTE,
    );
  });
});
