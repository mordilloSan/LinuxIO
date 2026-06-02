import React from "react";

import IndexerStatusDialog, {
  type IndexerStat,
} from "@/components/dialog/IndexerStatusDialog";
import { useBackgroundJobActions } from "@/hooks/backgroundJobs/useBackgroundJobActions";
import { useBackgroundJobIndexer } from "@/hooks/backgroundJobs/useBackgroundJobIndexer";

const IndexerDialog: React.FC = () => {
  const { closeIndexerDialog } = useBackgroundJobActions();
  const { indexers, isIndexerDialogOpen, lastIndexerResult, lastIndexerError } =
    useBackgroundJobIndexer();
  const activeIndexer = indexers[0];
  const isRunning = Boolean(activeIndexer);
  const success = !isRunning && Boolean(lastIndexerResult);
  const error = !isRunning ? lastIndexerError : null;
  const filesIndexed = isRunning
    ? (activeIndexer?.filesIndexed ?? 0)
    : (lastIndexerResult?.filesIndexed ?? 0);
  const dirsIndexed = isRunning
    ? (activeIndexer?.dirsIndexed ?? 0)
    : (lastIndexerResult?.dirsIndexed ?? 0);

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

  const progressStats: IndexerStat[] = [
    {
      value: filesIndexed.toLocaleString(),
      label: "Files indexed",
      valueColor: "primary.main",
      valueVariant: "h4",
    },
    {
      value: dirsIndexed.toLocaleString(),
      label: "Directories indexed",
      valueColor: "primary.main",
      valueVariant: "h4",
    },
  ];

  const successDescription = lastIndexerResult
    ? `Indexed ${lastIndexerResult.filesIndexed.toLocaleString()} files and ${lastIndexerResult.dirsIndexed.toLocaleString()} directories in ${(lastIndexerResult.durationMs / 1000).toFixed(2)}s`
    : undefined;

  return (
    <IndexerStatusDialog
      error={error}
      isRunning={isRunning}
      onClose={closeIndexerDialog}
      open={isIndexerDialogOpen}
      phaseLabel={getPhaseLabel()}
      progressStats={progressStats}
      showProgressStats={isRunning || success}
      success={success}
      successDescription={successDescription}
      title="Indexing Filesystem"
    />
  );
};

export default IndexerDialog;
