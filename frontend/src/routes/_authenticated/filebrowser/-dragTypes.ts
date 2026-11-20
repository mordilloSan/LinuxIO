// The dataTransfer type that carries file-browser paths in a drag inside the
// browser, as opposed to the browser's own "Files" type for desktop items.
export const INTERNAL_DRAG_TYPE = "application/x-linuxio-paths";

// Items dragged out of the Trash: each one's trashed path and the name it is
// shown under, which a drop restores it into a folder with. Its own type, so
// a drop target never mistakes it for a move.
export const TRASH_DRAG_TYPE = "application/x-linuxio-trash";
