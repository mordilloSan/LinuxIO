import React, { useCallback, useEffect, useRef, useState } from "react";

import { type ComposeProject, linuxio, type Stream, useStreamMux } from "@/api";
import IndexerStatusDialog, {
  type IndexerStat,
  type IndexerStatSection,
} from "@/components/dialog/IndexerStatusDialog";

const normalizeIndexedPath = (path: string) => {
  const trimmed = path.trim();
  if (!trimmed) {
    return "";
  }

  return trimmed.replace(/\/+$/, "") || "/";
};

const isPathInsideFolder = (path: string, folder: string) => {
  const normalizedPath = normalizeIndexedPath(path);
  const normalizedFolder = normalizeIndexedPath(folder);

  if (!normalizedPath || !normalizedFolder) {
    return false;
  }

  if (normalizedFolder === "/") {
    return normalizedPath.startsWith("/");
  }

  return (
    normalizedPath === normalizedFolder ||
    normalizedPath.startsWith(`${normalizedFolder}/`)
  );
};

const getComposeProjectPaths = (project: ComposeProject) => [
  project.working_dir,
  ...project.config_files,
];

interface DockerIndexerDialogProps {
  onClose: () => void;
  onComplete?: () => void;
  open: boolean;
}

interface IndexerProgress {
  current_path?: string;
  dirs_indexed: number;
  files_indexed: number;
  phase?: string;
}

interface FolderIndexerResult {
  dirs_indexed: number;
  duration_ms?: number;
  files_indexed: number;
  path: string;
  total_size?: number;
}

interface IndexerResult extends FolderIndexerResult {
  folders?: FolderIndexerResult[];
}

const DockerIndexerDialog: React.FC<DockerIndexerDialogProps> = ({
  open,
  onClose,
  onComplete,
}) => {
  const [progress, setProgress] = useState<IndexerProgress>({
    files_indexed: 0,
    dirs_indexed: 0,
    phase: "connecting",
  });
  const [isRunning, setIsRunning] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);
  const [result, setResult] = useState<IndexerResult | null>(null);
  const streamRef = useRef<Stream | null>(null);
  const abortControllerRef = useRef<AbortController | null>(null);
  const jobIdRef = useRef<string | null>(null);
  const hasCompletedRef = useRef(false);
  const closedByUserRef = useRef(false);

  const { isOpen: muxIsOpen } = useStreamMux();

  // Cancels are fire-and-forget; a plain job action reports nothing.
  const { mutate: cancelJob } = linuxio.jobs.cancel.useJobAction();

  const { mutate: runIndexer } = linuxio.docker.indexer.useJobStreamAction<
    IndexerResult,
    IndexerProgress
  >({
    signal: () => abortControllerRef.current?.signal,
    closeOnAbort: "none",
    openErrorMessage: "Failed to attach indexer operation",
    closeMessage: "Indexer stream closed unexpectedly",
    onJobStart: (job) => {
      // The dialog may have been closed while the job was being created;
      // cancel the orphaned job instead of tracking it.
      if (abortControllerRef.current?.signal.aborted !== false) {
        cancelJob({ jobId: job.id });
        return;
      }
      jobIdRef.current = job.id;
    },
    onOpen: (stream) => {
      streamRef.current = stream;
      closedByUserRef.current = false;
    },
    onProgress: (progressData) => {
      setProgress(progressData);
    },
    success: (indexerResult) => {
      hasCompletedRef.current = true;
      setResult(indexerResult);
      setSuccess(true);
      onComplete?.();
    },
    error: (err) => {
      if (closedByUserRef.current || err.name === "AbortError") {
        return;
      }
      hasCompletedRef.current = true;
      setError(err.message || "Indexing failed");
    },
    options: {
      onSettled: () => {
        streamRef.current = null;
        abortControllerRef.current = null;
        setIsRunning(false);
      },
    },
  });

  const { data: composeProjects = [], isPending: composeProjectsPending } =
    linuxio.docker.list_compose_projects.useQuery({
      enabled: open && success,
    });

  // Close stream helper
  const closeStream = useCallback(() => {
    if (streamRef.current) {
      closedByUserRef.current = true;
      streamRef.current.close();
      streamRef.current = null;
    }
    abortControllerRef.current?.abort();
    abortControllerRef.current = null;
  }, []);

  // Reset state helper
  const resetState = useCallback(() => {
    closeStream();
    setProgress({ files_indexed: 0, dirs_indexed: 0, phase: "connecting" });
    setIsRunning(true);
    setError(null);
    setSuccess(false);
    setResult(null);
    hasCompletedRef.current = false;
    closedByUserRef.current = false;
    jobIdRef.current = null;
  }, [closeStream]);

  // Cleanup stream when dialog closes
  useEffect(() => {
    if (!open) {
      closeStream();
    }
  }, [open, closeStream]);

  // Start the indexer job when the dialog opens.
  useEffect(() => {
    if (!open || !muxIsOpen) {
      return;
    }

    // Don't create duplicate streams or recreate after completion
    if (streamRef.current || jobIdRef.current || hasCompletedRef.current) {
      return;
    }

    abortControllerRef.current = new AbortController();
    runIndexer(undefined);
  }, [muxIsOpen, open, runIndexer]);

  const handleClose = () => {
    if (isRunning) {
      if (jobIdRef.current) {
        cancelJob({ jobId: jobIdRef.current });
      }
      closeStream();
    }
    onClose();
  };

  const getPhaseLabel = () => {
    switch (progress.phase) {
      case "connecting":
        return "Connecting to indexer...";
      case "indexing":
        return "Indexing Docker folders...";
      default:
        return "Processing...";
    }
  };

  const displayedFilesIndexed =
    success && result ? result.files_indexed : progress.files_indexed;
  const displayedDirsIndexed =
    success && result ? result.dirs_indexed : progress.dirs_indexed;

  const progressStats: IndexerStat[] = [
    {
      value: displayedFilesIndexed.toLocaleString(),
      label: "Files indexed",
      valueColor: "primary.main",
      valueVariant: "h4",
    },
    {
      value: displayedDirsIndexed.toLocaleString(),
      label: "Directories indexed",
      valueColor: "primary.main",
      valueVariant: "h4",
    },
  ];

  const folderResults =
    result?.folders && result.folders.length > 0
      ? result.folders
      : result
        ? [result]
        : [];

  const folderDetailSections: IndexerStatSection[] = folderResults.map(
    (folder, index) => {
      const stacksDiscovered = composeProjects.filter((project) =>
        getComposeProjectPaths(project).some((path) =>
          isPathInsideFolder(path, folder.path),
        ),
      ).length;

      return {
        title:
          folderResults.length > 1
            ? `Docker Folder ${index + 1}`
            : "Docker Folder",
        subtitle: folder.path,
        stats: [
          {
            value: folder.files_indexed.toLocaleString(),
            label: "Files indexed",
            valueColor: "primary.main",
            valueVariant: "h5",
          },
          {
            value: folder.dirs_indexed.toLocaleString(),
            label: "Directories indexed",
            valueColor: "primary.main",
            valueVariant: "h5",
          },
          {
            value: composeProjectsPending
              ? "..."
              : stacksDiscovered.toLocaleString(),
            label: "Stacks discovered",
            valueColor: "primary.main",
            valueVariant: "h5",
          },
        ],
      };
    },
  );

  const successDescription = result
    ? `Indexed ${result.files_indexed.toLocaleString()} files and ${result.dirs_indexed.toLocaleString()} directories in ${((result.duration_ms ?? 0) / 1000).toFixed(2)}s`
    : undefined;

  return (
    <IndexerStatusDialog
      detailSections={folderDetailSections}
      detailTitle={
        folderDetailSections.length > 1 ? "Docker Folder Results:" : undefined
      }
      error={error}
      isRunning={isRunning}
      onClose={handleClose}
      onExited={resetState}
      open={open}
      phaseLabel={getPhaseLabel()}
      progressStats={progressStats}
      showProgressStats={success || progress.phase === "indexing"}
      success={success}
      successDescription={successDescription}
      title="Indexing Docker Folders"
    />
  );
};

export default DockerIndexerDialog;
