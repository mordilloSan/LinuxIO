import React from "react";

import ReindexStatusDialog, {
  type ReindexStat,
} from "@/components/dialog/ReindexStatusDialog";
import { useFileTransfers } from "@/hooks/useFileTransfers";

const IndexerDialog: React.FC = () => {
  const {
    indexers,
    isIndexerDialogOpen,
    closeIndexerDialog,
    lastIndexerResult,
    lastIndexerError,
  } = useFileTransfers();
  const activeIndexer = indexers[0];
  const isRunning = Boolean(activeIndexer);
  const success = !isRunning && Boolean(lastIndexerResult);
  const error = !isRunning ? lastIndexerError : null;

  const getPhaseLabel = () => {
    if (isRunning) {
      switch (activeIndexer?.phase) {
        case "connecting":
          return "Connecting to indexer...";
        case "indexing":
          return "Indexing filesystem...";
        default:
          return "Processing...";
      }
    }

    if (success) {
      return "Indexing completed.";
    }

    if (error) {
      return "Indexing failed.";
    }

    return "Ready to index filesystem.";
  };

  const progressStats: ReindexStat[] = [
    {
      value: (activeIndexer?.filesIndexed ?? 0).toLocaleString(),
      label: "Files indexed",
      valueColor: "primary.main",
      valueVariant: "h4",
    },
    {
      value: (activeIndexer?.dirsIndexed ?? 0).toLocaleString(),
      label: "Directories indexed",
      valueColor: "primary.main",
      valueVariant: "h4",
    },
  ];

  const successDescription = lastIndexerResult
    ? `Indexed ${lastIndexerResult.filesIndexed.toLocaleString()} files and ${lastIndexerResult.dirsIndexed.toLocaleString()} directories in ${(lastIndexerResult.durationMs / 1000).toFixed(2)}s`
    : undefined;

  return (
    <ReindexStatusDialog
      open={isIndexerDialogOpen}
      onClose={closeIndexerDialog}
      title="Indexing Filesystem"
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

export default IndexerDialog;
