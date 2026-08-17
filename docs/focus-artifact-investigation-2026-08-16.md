# Focus artifact investigation handoff — 2026-08-16

## Status at handoff

- Branch: `dev/v0.21.0`
- HEAD when this was written: `23bc7542`
- Do not make commits on the user's behalf. Leave all commit decisions to the
  user.
- Stop treating every visible outline as the same bug. Today's screenshots
  contain at least two different focus indicators with different owners.
- The focus-related source changes are committed through `23bc7542`. The
  worktree was already dirty with unrelated frontend work when this document
  was created; do not discard or rewrite those changes.

## Executive summary

The strange white, stepped outline around a dock item is not an app-defined
border. It is Chromium's default user-agent focus outline on the
`a.app-dock-link` anchor. Chromium traces the anchor's visually overflowing
descendants: a transformed dock tile and an absolutely positioned/transformed
label. That produces the disconnected, angular contour seen around the tile and
the label.

The dark rectangle containing `Dashboard` or `Settings` is app-authored. It is
`.app-dock__label`, whose border, background, radius, and shadow come from
`dock.css` and the floating-surface variables.

The underlying lifecycle bug was also reproduced in a real headed Chromium
window. A pointer-focused dock link had `data-pointer-focus`; switching away
from the application produced `window.blur` followed by `focusout` while
`document.hasFocus()` was false. The old tracker removed the pointer-focus mark
on every `focusout`. On return, focus was restored while the tracker was in
keyboard modality, so the mark was not restored. A later key such as Shift made
the still-focused link match `:focus-visible`, which simultaneously:

1. showed the app's dock label through the focus selector; and
2. enabled Chromium's default anchor outline around the transformed descendants.

This explains why the artifact appeared after using another application and
returning, and why its shape looked unrelated to any CSS border in the app.

The small-screen blue circle around the tune/`Actions` button is a separate
paint: the app's intentional `2px` primary-colour
`.app-icon-btn:focus-visible` outline. It occurred when the mobile search
popover restored focus to its trigger after typing/Escape had switched the
browser to keyboard modality. Commit `541e411d` targets that narrower flow. It
does not fix the dock/window-reactivation lifecycle.

## What the screenshots established

### Desktop `Settings` label after returning to the app

The dark `Settings` box is the dock label. The screenshot alone was compatible
with stale `:hover`, stale focus, or both, which led to the first reactivation
fix being aimed primarily at hover state.

### Small-screen blue outline around `Actions`

This is the normal app-authored icon-button focus ring, painted after mobile
search closes and restores focus to the tune button. It motivated the
`RoutedTabContainer` change in `541e411d`. It should not be used as evidence for
the source of the dock's stepped white outline.

### Desktop stepped outline around `Dashboard`

This screenshot exposed the actual dock outline geometry. Inspection in
Chromium showed a default computed anchor outline (`outline-style: auto`) rather
than a project CSS border. The outer white contour follows the transformed tile
and label descendants; the inner dark `Dashboard` rectangle is
`.app-dock__label`.

The relevant DOM is effectively:

```text
nav.app-dock
  ul.app-dock__list
    li.app-dock__item
      a.app-dock-link
        span.app-dock__slot
          span.app-dock__dot
          span.app-dock__tile
          span.app-dock__label-anchor
            span.app-dock__label
```

Before `23bc7542`, `.app-dock-link` did not define an outline, so Chromium's
user-agent focus style was free to draw one. The tile is rendered at `64px` and
scaled/transformed by Motion, while the label anchor is absolutely positioned
and independently transformed. Those visual-overflow boxes account for the
outline's unusual shape.

## Direct attempts made today

These are the commits that directly attempted to prevent stale labels, rings,
or focus artifacts. The list is chronological.

| Time | Commit | Attempt | What it did not cover |
| --- | --- | --- | --- |
| 06:36 | `f39fb7b8` — `fix(Dock): update focus behavior for action tiles; add tests for keyboard-visible focus` | Replaced action-tile `:focus-within` label display with `:has(:focus-visible)` so a pointer click would not leave the action label visible. Added a CSS source-string test. | It only addressed the then-separate dock action wrapper. It did not track the origin of focus or model window deactivation. The action section was removed later the same day. |
| 10:59 | `246a0dec` — `fix(frontend): prevent stale overlays and pointer-focus artifacts` | Introduced the global input-modality tracker, installed it before first render, marked pointer-origin focus with `data-pointer-focus`, and globally suppressed outlines on marked non-text controls. It also hardened tooltip timers/focus behavior, dialog focus restoration, detached popover anchors, and select repositioning. | Its `focusout` handler always removed the pointer-focus mark. Real window deactivation also emits `focusout`, so the origin information was lost precisely during the reported workflow. |
| 11:45 | `fbeb9184` — `refactor: update focus-visible handling for dock and section header components` | Changed dock label display to `.app-dock-link:focus-visible:not([data-pointer-focus])`, and applied the same origin distinction to other focus-revealed UI. | Correctly depended on the marker, but could not work after the tracker discarded that marker on window `focusout`. Its dock test checked selector text, not browser behavior or geometry. |
| 20:25 | `59e8e53a` — `fix(frontend): prevent stale dock labels after window reactivation` | Added root `data-pointer-active`, set by live mouse/pen activity and cleared on window blur or document hiding. Dock hover labels now require both `:hover` and that root marker. Added unit and browser coverage. | This addresses Chromium reviving `:hover` without a fresh pointer move, but not the focus path. The browser fixture was only a bare link plus label, and the test manually dispatched `window.blur`; it did not reproduce the real `focusout` that removed `data-pointer-focus`. |
| 22:03 | `541e411d` — `feat: implement mobile search focus restoration and pointer focus handling` | Recorded whether mobile search was opened from a pointer. When search closed, it restored focus to the `Actions` trigger and reapplied `data-pointer-focus` for the pointer-opened case. Added a browser test. | This is a targeted fix for the small-screen tune-button ring, not for dock window reactivation. It added state and component-specific marker manipulation because the second screenshot was initially treated as the same problem. |
| 22:22 | `23bc7542` — `feat: enhance focus handling in dock components and tests` | Preserved `data-pointer-focus` when `focusout` occurs with no `relatedTarget` while `document.hasFocus()` is false; marked pointer takeover when an already-focused control receives `pointerdown`; disabled Chromium's default outline on focused dock links; replaced the fake dock fixture with the real `DockTile`/magnification structure; and added browser/unit scenarios based on the measured lifecycle. | This is the current attempt. The automated window lifecycle is still simulated from measured events because headless Playwright has no real desktop window manager. It has not yet been visually rechecked after the fix using the same real Alt-Tab/window-switch setup. See the review concern below. |

## Why `UsersPage` changed

`541e411d` changed
`frontend/src/test/browser/fixture/routes/UsersPage.tsx`, not the production
Accounts/Users page. The fixture previously rendered only a heading. It was
expanded to mount `RoutedTabSearch` and `AppHeaderSearch` so the new Playwright
test could exercise the real mobile `Actions -> Search -> Escape -> restore
focus` flow on `/accounts`.

That fixture change was test scaffolding for the small-screen ring hypothesis.
It was not needed to explain the later dock screenshot, and it should be judged
only by whether the separate mobile-search regression test is worth retaining.

## Related same-day commits that changed the surface

These commits are context, not direct attempts to fix the reported artifact:

- `86f926c1` at 09:58 changed dock tile rasterisation and magnification geometry.
- `b1e90e16` at 10:23 refactored dock magnification for React Compiler
  compatibility.
- `8be850b3` at 11:27 dissolved the dock's separate action section, making
  Settings a normal navigation tile and removing action-specific focus rules.
- `db400b95` at 20:02 added configurable dock tile colours and touched the dock
  structure/styles without changing the focus policy.
- `e7aa7dfa` at 21:40 introduced the mobile `Actions`/search-popover flow whose
  focus restoration produced the small-screen tune-button ring.
- `19ef92c4` at 22:05 added Escape-to-clear behavior to `AppHeaderSearch`. Escape
  is also a keyboard-modality signal, so it is relevant to the mobile sequence,
  but this commit was not itself an outline fix.

## Earlier origin of the dock geometry

The current behavior was built across commits from 2026-08-15:

- `02713c29` introduced the new dock mode and label display from hover/focus.
- `3315ac58` and `5f859ee5` developed the dock navigation/action slots and
  `DockTile` structure.
- `afd06b39` moved magnification off the layout path and introduced the current
  transformed tile plus independently positioned label-anchor geometry.

These commits explain why a default anchor outline can surround several
apparently disconnected rectangles. They are historical causes/geometry, not
today's attempted fixes.

## Reproduction evidence

The decisive reproduction used headed Google Chrome inside a nested X server
with a real window manager (Xephyr display `:99` plus Muffin). This was necessary
because headless pages do not reproduce desktop focus transfer accurately.

Starting with a dock link focused by pointer, the observed event/state sequence
was:

```text
pointer focus
  activeElement = dock link
  data-pointer-focus present

switch away
  keydown from the window switch can set modality = keyboard
  window blur
  focusout
    relatedTarget = null
    document.hasFocus() = false
    old handler removed data-pointer-focus

return to app
  window focus
  focusin on the same dock link
  modality is keyboard, so the old handler does not restore data-pointer-focus

next qualifying key (Shift was sufficient)
  link matches :focus-visible
  dock label opacity becomes 1
  Chromium computes its default auto outline
  screenshot matches the stepped Dashboard artifact
```

A second tracker gap was also found: clicking a control that is already focused
does not emit another `focusin`. Before `23bc7542`, that pointer takeover could
therefore leave the element classified as keyboard-focused.

## Why earlier tests were reassuring but incomplete

1. The dock unit tests primarily read `dock.css` as text and asserted that a
   selector existed. They did not execute Chromium's focus heuristic.
2. The first browser dock fixture rendered only:

   ```text
   a.app-dock-link
     span.app-dock__label
   ```

   It omitted `.app-dock__slot`, the transformed `.app-dock__tile`, the
   absolutely positioned `.app-dock__label-anchor`, and the magnification
   provider. It could not reproduce the outline geometry from the screenshot.
3. The first reactivation browser test manually dispatched `window.blur`. A
   real Chromium window switch also emitted `focusout`; that second event was
   the one that deleted `data-pointer-focus`.
4. `page.bringToFront()` in headless Playwright is not equivalent to changing
   desktop windows through a window manager.
5. JSDOM's `document.hasFocus()` behavior differs from the real browser. One new
   unit test initially failed for that reason; the current condition also uses
   `FocusEvent.relatedTarget` to distinguish ordinary in-document focus moves.

The fixture in `23bc7542` now uses the real dock DOM and transformed descendants.
Its lifecycle test still synthesizes the event sequence, but that sequence came
from the headed-Chromium measurement above rather than from assumption.

## Current implementation and open review concern

At `23bc7542`, the intended invariants are:

- pointer-origin focus remains marked across whole-window deactivation;
- a genuine in-document move to another focus target removes the old mark;
- clicking an already-focused keyboard-origin control changes its origin to
  pointer;
- keyboard-origin dock focus still shows the dock label;
- the dock uses that label as its focus indicator and suppresses Chromium's
  malformed descendant outline;
- stale dock hover cannot reactivate until a fresh mouse/pen event occurs.

One concrete concern was noticed after the code was written and must be reviewed
before adding anything else: the new `pointerdown` path marks
`document.activeElement` whenever that element appears in the event's composed
path. On an initial page click, `document.activeElement` can be
`document.body`, and `body` is normally in the composed path. The current code
can therefore add `data-pointer-focus` to `body`. This may be harmless in most
states, but it is unintended global state and was not fixed before the
investigation was stopped. A likely correction is to exclude `body` and
`document.documentElement`, but it should be tested rather than patched by
reflex.

Also review the accessibility decision in `.app-dock-link:focus-visible {
outline: none; }`. The label remains visible for genuine keyboard focus and is
currently treated as the replacement focus indicator. Confirm that this is
sufficiently obvious and contrasted; otherwise use a deliberate tile-local
focus style that does not trace the transformed label.

## Test evidence for the current attempt

During the work that became `23bc7542`:

- The first `make check-frontend` run failed one newly added unit test because
  JSDOM reported `document.hasFocus()` differently from Chromium. The focusout
  condition was revised to include `event.relatedTarget`.
- A subsequent `make check-frontend` passed: 170 test files, 827 tests.
- `make test-frontend-browser` passed: 17 browser tests.
- A separate fresh final verification run was started but interrupted when the
  user stopped the fix work and requested this handoff.

Do not describe this as a completed real-window regression test. The pre-fix
bug was reproduced with a real window manager; the post-fix behavior was covered
by unit tests and a synthetic browser lifecycle, but the exact visual
switch-away/return sequence still needs a post-fix headed check.

## Recommended next-session approach

1. Do not add another selector or component-specific flag first. Reproduce the
   exact switch-away/return sequence against `23bc7542` and record state at each
   event: `document.activeElement`, `document.hasFocus()`, `relatedTarget`,
   `data-pointer-focus`, root `data-pointer-active`, `:focus-visible`, computed
   outline, and label opacity.
2. Confirm separately whether the mobile `Actions` ring still reproduces. Keep
   it separate from the dock issue in both diagnosis and tests.
3. Review the `body`-marking concern in `handlePointerDown` and add a focused
   regression test before changing the tracker.
4. Test all tracker boundaries: pointer focus, pointer takeover of already
   focused keyboard focus, normal focus movement within the document, actual
   window deactivation/reactivation, keyboard Tab focus, Enter/Space activation,
   and text-entry controls.
5. Decide explicitly whether a dock keyboard focus should be represented by
   the label alone or by a custom tile-local ring as well. Do not restore the
   user-agent anchor outline around visually overflowing descendants.
6. Only after the behavior is understood, simplify or remove redundant earlier
   defenses. In particular, decide whether `data-pointer-active`, the mobile
   search's manual marker restoration, and the global origin tracker each still
   have distinct justified responsibilities.
7. For any frontend source change, run `make check-frontend`; for claims about
   the browser behavior, also run `make test-frontend-browser` and perform the
   real headed window-switch check. Report those forms of evidence separately.

## Primary files to inspect

- `frontend/src/utils/inputModality.ts`
- `frontend/src/utils/inputModality.test.ts`
- `frontend/src/theme/variables.css`
- `frontend/src/index.tsx`
- `frontend/src/routes/_authenticated/-components/dock/dock.css`
- `frontend/src/routes/_authenticated/-components/dock/DockTile.tsx`
- `frontend/src/test/browser/accessibility.spec.ts`
- `frontend/src/test/browser/fixture/routes/AccessibilityPage.tsx`
- `frontend/src/components/tabbar/RoutedTabContainer.tsx`
- `frontend/src/test/browser/mobile-search.spec.ts`
- `frontend/src/test/browser/fixture/routes/UsersPage.tsx`
- `frontend/src/components/ui/app-icon-button.css`

