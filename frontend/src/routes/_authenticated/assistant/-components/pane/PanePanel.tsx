import type { PaneView } from "@/api/acp";

import DockPanel from "../dock/DockPanel";
import PaneTree from "./PaneTree";
import { paneTitle } from "./readPaneTree";

/**
 * A Claude Code plugin's pane, drawn read-only on one of the dock's cards.
 */
export default function PanePanel({
  pane,
  room,
  open,
  onOpenChange,
  running,
}: {
  pane: PaneView;
  room: number | null;
  open: boolean | null;
  onOpenChange: (open: boolean) => void;
  running: boolean;
}) {
  return (
    <DockPanel
      drawing={pane.tree}
      onOpenChange={onOpenChange}
      open={open}
      room={room}
      running={running}
      title={paneTitle(pane)}
    >
      <PaneTree tree={pane.tree} />
    </DockPanel>
  );
}
