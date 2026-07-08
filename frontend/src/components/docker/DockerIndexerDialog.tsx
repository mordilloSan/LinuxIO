import React, { useCallback, useEffect, useRef, useState } from "react";

import { type ComposeProject, linuxio, useStreamMux } from "@/api";
import IndexerStatusDialog, {
  type IndexerStat,
  type IndexerStatSection,
} from "@/components/dialog/IndexerStatusDialog";
import { JOB_TYPE_DOCKER_INDEXER } from "@/constants/backgroundJobTypes";
import { useActiveJobRecovery } from "@/hooks/backgroundJobs/useActiveJobRecovery";

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
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);
  const [result, setResult] = useState<IndexerResult | null>(null);
  // Started-guard: one run per dialog open (reset on dialog exit). The abort
  // controller detaches the stream (closeOnAbort: "close"); jobIdRef is the
  // cancel handle for a user-initiated close while indexing.
  const startedRef = useRef(false);
  const abortRef = useRef<AbortController | null>(null);
  const jobIdRef = useRef<string | null>(null);

  const { isOpen: muxIsOpen } = useStreamMux();

  const isRunning = !success && !error;

  // Cancels are fire-and-forget; a plain job action reports nothing.
  const { mutate: cancelJob } = linuxio.jobs.cancel.useJobAction();

  const indexer = linuxio.docker.indexer.useJobStreamAction<
    IndexerResult,
    IndexerProgress
  >({
    signal: () => abortRef.current?.signal,
    closeOnAbort: "close",
    openErrorMessage: "Failed to attach indexer operation",
    closeMessage: "Indexer stream closed unexpectedly",
    onJobStart: (job) => {
      // The dialog may have been closed while the job was being created;
      // cancel the orphaned job instead of tracking it.
      if (abortRef.current?.signal.aborted !== false) {
        cancelJob({ jobId: job.id });
        return;
      }
      jobIdRef.current = job.id;
    },
    onProgress: (progressData) => {
      setProgress(progressData);
    },
    success: (indexerResult) => {
      setResult(indexerResult);
      setSuccess(true);
      onComplete?.();
    },
    error: (err) => {
      if (err.name === "AbortError") {
        return;
      }
      setError(err.message || "Indexing failed");
    },
  });

  const { data: composeProjects = [], isPending: composeProjectsPending } =
    linuxio.docker.list_compose_projects.useQuery({
      enabled: open && success,
    });

  // Reset state helper
  const resetState = useCallback(() => {
    abortRef.current?.abort();
    abortRef.current = null;
    startedRef.current = false;
    jobIdRef.current = null;
    setProgress({ files_indexed: 0, dirs_indexed: 0, phase: "connecting" });
    setError(null);
    setSuccess(false);
    setResult(null);
  }, []);

  // Detach from the stream when the dialog closes or unmounts.
  useEffect(() => {
    if (!open) {
      abortRef.current?.abort();
    }
  }, [open]);
  useEffect(() => () => abortRef.current?.abort(), []);

  // One recovery scan per dialog open decides between the two start paths:
  // adopt an indexer that is already running (page reloaded mid-index) or
  // start a fresh one.
  const beginRun = (run: () => void) => {
    if (startedRef.current) return;
    startedRef.current = true;
    abortRef.current = new AbortController();
    run();
  };
  useActiveJobRecovery({
    type: JOB_TYPE_DOCKER_INDEXER,
    scanKey: open && muxIsOpen ? "docker-indexer" : null,
    match: () => true,
    onRecover: (job) => beginRun(() => indexer.attach(job, undefined)),
    onMiss: () => beginRun(() => indexer.mutate(undefined)),
  });

  const handleClose = () => {
    if (isRunning && startedRef.current) {
      if (jobIdRef.current) {
        cancelJob({ jobId: jobIdRef.current });
      }
      abortRef.current?.abort();
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
