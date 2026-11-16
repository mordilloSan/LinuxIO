import { useState } from "react";

import AppConfirmDialog from "@/components/ui/AppConfirmDialog";
import { useLatestRef } from "@/hooks/useLatestRef";
import { baseName, parentPath } from "@/utils/path";

import {
  type NoTrashPrompt as Prompt,
  settleNoTrashPrompt,
  useNextNoTrashPrompt,
} from "../-hooks/noTrashPrompts";

interface NoTrashPromptProps {
  /** Deletes the items for good; it reports its own result. */
  onDelete: (paths: string[]) => Promise<unknown>;
}

// The question may come long after the user moved on, so it names the items
// and their folder itself instead of leaning on what is on screen.
const describe = (paths: readonly string[]) => {
  const folders = new Set(paths.map(parentPath));
  const where = folders.size === 1 ? ` in ${[...folders][0]}` : "";
  return paths.length === 1
    ? `${baseName(paths[0])}${where} cannot go to the Trash. Delete it permanently? This cannot be undone.`
    : `${paths.length} items${where} cannot go to the Trash. Delete them permanently? This cannot be undone.`;
};

/**
 * Asks once for each move to Trash that ended with items whose disk cannot
 * hold a trash whether to delete those items for good. Nothing is deleted
 * without the answer, and Cancel leaves them where they are.
 */
const NoTrashPrompt = ({ onDelete }: NoTrashPromptProps) => {
  const next = useNextNoTrashPrompt();
  // The question on screen. It keeps its words while it fades out, and the
  // next one opens only after that, so a second click on Delete never
  // answers a question the user has not read.
  const [shown, setShown] = useState<Prompt | null>(null);
  const [open, setOpen] = useState(false);
  if (shown === null && next !== null) {
    setShown(next);
    setOpen(true);
  }
  const openRef = useLatestRef(open);

  const answer = (confirmed: boolean) => {
    if (!shown || !openRef.current) return;
    setOpen(false);
    settleNoTrashPrompt(shown.id);
    if (confirmed) {
      onDelete([...shown.paths]).catch(() => {
        // The delete reports its own failure.
      });
    }
  };

  return (
    <AppConfirmDialog
      confirmText="Delete"
      destructive
      message={shown ? describe(shown.paths) : ""}
      onClose={() => answer(false)}
      onConfirm={() => answer(true)}
      onExited={() => setShown(null)}
      open={open}
      title="Delete permanently"
    />
  );
};

export default NoTrashPrompt;
