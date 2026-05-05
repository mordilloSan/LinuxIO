import React, { useState, useEffect, useRef, useCallback } from "react";

import { linuxio, useStreamMux, openJobAttachStream, type Stream } from "@/api";
import IndexerStatusDialog, {
  type IndexerStat,
} from "@/components/dialog/IndexerStatusDialog";
import { useStreamResult } from "@/hooks/useStreamResult";

const JOB_TYPE_DOCKER_INDEXER = "docker.indexer";

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

interface IndexerResult {
  path: string;
  files_indexed: number;
  dirs_indexed: number;
  duration_ms: number;
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

  const { data: composeProjects = [] } =
    linuxio.docker.list_compose_projects.useQuery({
      enabled: open && success,
    });

  const stacksSummary = success
    ? {
        total: composeProjects.length,
        running: composeProjects.filter((p) => p.status === "running").length,
        stopped: composeProjects.filter((p) => p.status === "stopped").length,
      }
    : null;

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

  const progressStats: IndexerStat[] = [
    {
      value: progress.files_indexed.toLocaleString(),
      label: "Files indexed",
      valueColor: "primary.main",
      valueVariant: "h4",
    },
    {
      value: progress.dirs_indexed.toLocaleString(),
      label: "Directories indexed",
      valueColor: "primary.main",
      valueVariant: "h4",
    },
  ];

  const summaryStats: IndexerStat[] = stacksSummary
    ? [
        {
          value: stacksSummary.total,
          label: "Total stacks",
          valueColor: "primary.main",
          valueVariant: "h5",
        },
        {
          value: stacksSummary.running,
          label: "Running",
          valueColor: "success.main",
          valueVariant: "h5",
        },
        {
          value: stacksSummary.stopped,
          label: "Stopped",
          valueColor: "text.secondary",
          valueVariant: "h5",
        },
      ]
    : [];

  const successDescription = result
    ? `Indexed ${result.files_indexed.toLocaleString()} files and ${result.dirs_indexed.toLocaleString()} directories in ${(result.duration_ms / 1000).toFixed(2)}s`
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
      showProgressStats={progress.phase === "indexing"}
      successDescription={successDescription}
      summaryTitle={stacksSummary ? "Docker Compose Stacks Found:" : undefined}
      summaryStats={summaryStats}
    />
  );
};

export default DockerIndexerDialog;
