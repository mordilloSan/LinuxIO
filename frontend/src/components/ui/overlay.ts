// Overlay portals that participate in the Escape stack: only the last one in
// DOM order may close on Escape. Shared by AppDialog, AppFullscreenDialog and
// the document-level shortcut hooks that must stay quiet while an overlay is
// open. A plain module so hooks can use it without importing a component.
export const OVERLAY_ROOT_SELECTOR =
  ".app-dialog-root, .app-fullscreen-dialog-root";
