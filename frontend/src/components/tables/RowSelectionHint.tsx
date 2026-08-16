import { Icon } from "@iconify/react";

import AppTypography from "@/components/ui/AppTypography";

/**
 * Teaches the row-selection gestures on a table that has no checkbox column.
 *
 * Selecting a row is a double click and selecting them all is Ctrl/Cmd-A —
 * neither leaves anything on screen to find, and the row tint is only feedback
 * once you already know the gesture. Render this wherever the page shows its
 * bulk action, so the hint occupies that spot until a selection replaces it:
 *
 *     {selected.size > 0 ? <DeleteAction /> : rows.length > 0 && <RowSelectionHint />}
 *
 * See `docs/table-row-gestures.md`.
 */
const RowSelectionHint = () => (
  <div
    style={{
      display: "flex",
      alignItems: "center",
      gap: 6,
      minWidth: 0,
    }}
  >
    <Icon
      color="var(--app-palette-text-disabled)"
      height={16}
      icon="mdi:gesture-double-tap"
      width={16}
    />
    <AppTypography
      color="text.secondary"
      style={{ fontSize: "0.75rem" }}
      variant="body2"
    >
      Double-click a row to select it
    </AppTypography>
  </div>
);

export default RowSelectionHint;
