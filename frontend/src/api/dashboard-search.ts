/**
 * The dashboard's search: `?dialog=` opens one of its dialogs, so a link, a
 * reload or a bookmark opens it. It lives below the routes because a toast
 * links to a dialog by it (toast-meta.ts), and api/ must not import routes;
 * the dashboard route validates its search with validateDashboardSearch.
 */

/** The `?dialog=` value that opens the Memory overflow dialog. */
export const MEMORY_OVERFLOW_DIALOG = "memory-overflow";

export interface DashboardSearch {
  dialog?: typeof MEMORY_OVERFLOW_DIALOG;
}

export const validateDashboardSearch = (
  search: Record<string, unknown>,
): DashboardSearch =>
  // A dialog the dashboard does not know is dropped, not rejected.
  search.dialog === MEMORY_OVERFLOW_DIALOG
    ? { dialog: MEMORY_OVERFLOW_DIALOG }
    : {};
