import IndexerStatusDialog, {
  type IndexerStat,
} from "@/components/dialog/IndexerStatusDialog";
import { indexerPhaseLabel } from "@/hooks/backgroundJobs/indexerProgress";
import { useBackgroundJobActions } from "@/hooks/backgroundJobs/useBackgroundJobActions";
import { useBackgroundJobIndexer } from "@/hooks/backgroundJobs/useBackgroundJobIndexer";
import { formatFileSize } from "@/utils/formaters";

const IndexerDialog = () => {
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
  const indexedSize = isRunning
    ? (activeIndexer?.bytesIndexed ?? 0)
    : (lastIndexerResult?.totalSize ?? 0);

  const getPhaseLabel = () => {
    if (isRunning && activeIndexer) {
      return indexerPhaseLabel(activeIndexer);
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
    {
      value: formatFileSize(indexedSize, 1, "0 Bytes"),
      label: isRunning ? "Data indexed" : "Indexed size",
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
