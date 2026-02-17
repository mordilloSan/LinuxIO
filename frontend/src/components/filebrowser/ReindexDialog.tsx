import React from "react";

import ReindexStatusDialog, {
  type ReindexStat,
} from "@/components/dialog/ReindexStatusDialog";
import { useFileTransfers } from "@/hooks/useFileTransfers";

const ReindexDialog: React.FC = () => {
  const {
    reindexes,
    isReindexDialogOpen,
    closeReindexDialog,
    lastReindexResult,
    lastReindexError,
  } = useFileTransfers();
  const activeReindex = reindexes[0];
  const isRunning = Boolean(activeReindex);
  const success = !isRunning && Boolean(lastReindexResult);
  const error = !isRunning ? lastReindexError : null;

  const getPhaseLabel = () => {
    if (isRunning) {
      switch (activeReindex?.phase) {
        case "connecting":
          return "Connecting to indexer...";
        case "indexing":
          return "Indexing filesystem...";
        default:
          return "Processing...";
      }
    }

    if (success) {
      return "Reindex completed.";
    }

    if (error) {
      return "Reindex failed.";
    }

    return "Ready to reindex filesystem.";
  };

  const progressStats: ReindexStat[] = [
    {
      value: (activeReindex?.filesIndexed ?? 0).toLocaleString(),
      label: "Files indexed",
      valueColor: "primary.main",
      valueVariant: "h4",
    },
    {
      value: (activeReindex?.dirsIndexed ?? 0).toLocaleString(),
      label: "Directories indexed",
      valueColor: "primary.main",
      valueVariant: "h4",
    },
  ];

  const successDescription = lastReindexResult
    ? `Indexed ${lastReindexResult.filesIndexed.toLocaleString()} files and ${lastReindexResult.dirsIndexed.toLocaleString()} directories in ${(lastReindexResult.durationMs / 1000).toFixed(2)}s`
    : undefined;

  return (
    <ReindexStatusDialog
      open={isReindexDialogOpen}
      onClose={closeReindexDialog}
      title="Reindexing Filesystem"
      isRunning={isRunning}
      success={success}
      error={error}
      phaseLabel={getPhaseLabel()}
      progressStats={progressStats}
      showProgressStats={isRunning}
      successDescription={successDescription}
    />
  );
};

export default ReindexDialog;
