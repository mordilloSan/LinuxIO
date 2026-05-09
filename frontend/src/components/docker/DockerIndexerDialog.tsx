import React, { useState, useEffect, useRef, useCallback } from "react";

import {
  linuxio,
  useStreamMux,
  openJobAttachStream,
  type ComposeProject,
  type Stream,
} from "@/api";
import IndexerStatusDialog, {
  type IndexerStat,
  type IndexerStatSection,
} from "@/components/dialog/IndexerStatusDialog";
import { useStreamResult } from "@/hooks/useStreamResult";

const JOB_TYPE_DOCKER_INDEXER = "docker.indexer";

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
  open: boolean;
  onClose: () => void;
  onComplete?: () => void;
}

interface IndexerProgress {
  files_indexed: number;
  dirs_indexed: number;
  current_path?: string;
  phase?: string;
}

interface FolderIndexerResult {
  path: string;
  files_indexed: number;
  dirs_indexed: number;
  duration_ms?: number;
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
  const { run: runStreamResult } = useStreamResult();

  const { isOpen: muxIsOpen } = useStreamMux();

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

  // Open stream when dialog opens.
  useEffect(() => {
    if (!open || !muxIsOpen) {
      return;
    }

    // Don't create duplicate streams or recreate after completion
    if (streamRef.current || jobIdRef.current || hasCompletedRef.current) {
      return;
    }

    const abortController = new AbortController();
    abortControllerRef.current = abortController;
    let cancelled = false;

    void (async () => {
      try {
        const job = await linuxio.jobs.start.call(JOB_TYPE_DOCKER_INDEXER);
        if (cancelled) {
          void linuxio.jobs.cancel.call(job.id).catch(() => undefined);
          return;
        }
        jobIdRef.current = job.id;
        await runStreamResult<IndexerResult, IndexerProgress>({
          open: () => openJobAttachStream(job.id),
          signal: abortController.signal,
          closeOnAbort: "none",
          openErrorMessage: "Failed to attach indexer operation",
          closeMessage: "Indexer stream closed unexpectedly",
          onOpen: (stream) => {
            streamRef.current = stream;
            closedByUserRef.current = false;
          },
          onProgress: (progressData) => {
            setProgress(progressData);
          },
          onSuccess: (indexerResult) => {
            hasCompletedRef.current = true;
            setResult(indexerResult);
            setSuccess(true);
            onComplete?.();
          },
        });
      } catch (err: unknown) {
        if (closedByUserRef.current) {
          return;
        }
        hasCompletedRef.current = true;
        const errorMessage =
          err instanceof Error ? err.message : "Indexing failed";
        setError(errorMessage);
      } finally {
        streamRef.current = null;
        abortControllerRef.current = null;
        setIsRunning(false);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [muxIsOpen, open, onComplete, runStreamResult]);

  const handleClose = () => {
    if (isRunning) {
      if (jobIdRef.current) {
        void linuxio.jobs.cancel.call(jobIdRef.current).catch(() => undefined);
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
      open={open}
      onClose={handleClose}
      onExited={resetState}
      title="Indexing Docker Folders"
      isRunning={isRunning}
      success={success}
      error={error}
      phaseLabel={getPhaseLabel()}
      progressStats={progressStats}
      showProgressStats={success || progress.phase === "indexing"}
      successDescription={successDescription}
      detailTitle={
        folderDetailSections.length > 1 ? "Docker Folder Results:" : undefined
      }
      detailSections={folderDetailSections}
    />
  );
};

export default DockerIndexerDialog;
