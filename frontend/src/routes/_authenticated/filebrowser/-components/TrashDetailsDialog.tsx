import { Icon } from "@iconify/react";

import type { TrashEntry } from "@/api";
import CardIconHeader from "@/components/cards/CardIconHeader";
import FileBrowserDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import AppChip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTypography from "@/components/ui/AppTypography";
import { GAP_LG, GAP_MD } from "@/theme/constants";
import { formatDate, formatFileSize } from "@/utils/formatters";

import { trashOriginalFolder, trashShownName } from "../-trashEntries";
import { DetailRow } from "./FileDetail";
import FileIcon from "./FileIcon";

interface TrashDetailsDialogProps {
  /** The items shown, read from the live list. */
  entries: TrashEntry[];
  onClose: () => void;
  onDelete: (entries: TrashEntry[]) => void;
  onRestore: (entries: TrashEntry[]) => void;
  onRestoreTo: (entries: TrashEntry[]) => void;
  open: boolean;
}

const plural = (count: number, word: string) =>
  `${count.toLocaleString()} ${word}${count === 1 ? "" : "s"}`;

const grid = {
  display: "grid",
  gap: GAP_MD,
  gridTemplateColumns: "repeat(auto-fit, minmax(160px, 1fr))",
} as const;

const OneItem = ({ entry }: { entry: TrashEntry }) => {
  const folder = trashOriginalFolder(entry);
  return (
    <>
      <CardIconHeader
        icon={
          <FileIcon
            filename={trashShownName(entry)}
            isDirectory={entry.isDir}
            size={28}
          />
        }
        iconTint
        right={
          <AppChip
            label={entry.isDir ? "Folder" : "File"}
            size="small"
            variant="soft"
          />
        }
        subtitle={entry.originalPath || "Original location unknown"}
        title={trashShownName(entry)}
      />
      <div style={grid}>
        <DetailRow label="Original location" value={folder || "Unknown"} />
        <DetailRow
          label="Date deleted"
          value={entry.deletedAt ? formatDate(entry.deletedAt) : "Unknown"}
        />
        <DetailRow
          label="Size"
          value={entry.isDir ? "Not listed" : formatFileSize(entry.size)}
        />
        <DetailRow label="Kind" value={entry.isDir ? "Folder" : "File"} />
      </div>
    </>
  );
};

const SeveralItems = ({ entries }: { entries: TrashEntry[] }) => {
  const files = entries.filter((entry) => !entry.isDir);
  const folders = entries.length - files.length;
  return (
    <>
      <CardIconHeader
        icon={<Icon height={28} icon="mdi:delete-outline" width={28} />}
        iconTint
        subtitle={[
          files.length ? plural(files.length, "file") : "",
          folders ? plural(folders, "folder") : "",
        ]
          .filter(Boolean)
          .join(", ")}
        title={plural(entries.length, "item")}
      />
      <div style={grid}>
        <DetailRow label="Items" value={entries.length.toLocaleString()} />
        <DetailRow
          label="Total size of files"
          value={formatFileSize(
            files.reduce((total, entry) => total + entry.size, 0),
          )}
        />
      </div>
    </>
  );
};

/**
 * The properties of trashed items, with what can be done to them. A trashed
 * item is not opened: this is what opening one shows.
 */
const TrashDetailsDialog = ({
  entries,
  onClose,
  onDelete,
  onRestore,
  onRestoreTo,
  open,
}: TrashDetailsDialogProps) => (
  <FileBrowserDialog fullWidth maxWidth="sm" onClose={onClose} open={open}>
    <AppDialogTitle
      style={{
        alignItems: "center",
        display: "flex",
        justifyContent: "space-between",
      }}
    >
      Properties
      <AppIconButton aria-label="Close dialog" onClick={onClose} size="small">
        <Icon height={18} icon="mdi:close" width={18} />
      </AppIconButton>
    </AppDialogTitle>
    <AppDialogContent>
      <div style={{ display: "flex", flexDirection: "column", gap: GAP_LG }}>
        {entries.length === 0 ? (
          <AppTypography color="text.secondary" variant="body2">
            These items are no longer in the Trash.
          </AppTypography>
        ) : entries.length === 1 ? (
          <OneItem entry={entries[0]} />
        ) : (
          <SeveralItems entries={entries} />
        )}
      </div>
    </AppDialogContent>
    <AppDialogActions>
      <AppButton
        color="error"
        disabled={entries.length === 0}
        onClick={() => onDelete(entries)}
      >
        Delete permanently
      </AppButton>
      <AppButton
        disabled={entries.length === 0}
        onClick={() => onRestoreTo(entries)}
      >
        Restore to…
      </AppButton>
      <AppButton
        disabled={entries.length === 0}
        onClick={() => onRestore(entries)}
        variant="contained"
      >
        Restore
      </AppButton>
    </AppDialogActions>
  </FileBrowserDialog>
);

export default TrashDetailsDialog;
