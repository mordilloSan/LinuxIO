import React, { useState, useEffect, useRef, useCallback } from "react";

import {
  linuxio,
  useStreamMux,
  openDockerIndexerStream,
  openDockerIndexerAttachStream,
  type Stream,
} from "@/api";
import IndexerStatusDialog, {
  type IndexerStat,
} from "@/components/dialog/IndexerStatusDialog";
import { useStreamResult } from "@/hooks/useStreamResult";

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

const isIndexerConflictError = (error: unknown): boolean => {
  const message =
    error instanceof Error ? error.message.toLowerCase().trim() : "";
  const code =
    typeof error === "object" && error !== null && "code" in error
      ? Number((error as { code?: unknown }).code)
      : null;

  return (
    code === 409 ||
    message.includes("already running") ||
    message.includes("conflict")
  );
};

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
  }, [closeStream]);

  // Cleanup stream when dialog closes
  useEffect(() => {
    if (!open) {
      closeStream();
    }
  }, [open, closeStream]);

  // Bind stream result handlers and track completion
  const bindStream = useCallback(
    (stream: Stream) => {
      const runBoundStream = (activeStream: Stream) => {
        streamRef.current = activeStream;
        closedByUserRef.current = false;

        void runStreamResult<IndexerResult, IndexerProgress>({
          open: () => activeStream,
          onProgress: (progressData) => {
            setProgress(progressData);
          },
          closeMessage: "Indexer stream closed unexpectedly",
        })
          .then((indexerResult) => {
            hasCompletedRef.current = true;
            setResult(indexerResult);
            setSuccess(true);
            onComplete?.();
          })
          .catch((err: unknown) => {
            if (closedByUserRef.current) {
              return;
            }

            if (isIndexerConflictError(err)) {
              const attachStream = openDockerIndexerAttachStream();
              if (attachStream) {
                setError(null);
                setIsRunning(true);
                runBoundStream(attachStream);
                return;
              }
            }

            hasCompletedRef.current = true;
            const errorMessage =
              err instanceof Error ? err.message : "Indexing failed";
            setError(errorMessage);
          })
          .finally(() => {
            if (streamRef.current === activeStream) {
              streamRef.current = null;
              setIsRunning(false);
            }
          });
      };

      runBoundStream(stream);
    },
    [onComplete, runStreamResult],
  );

  // Open stream when dialog opens.
  useEffect(() => {
    if (!open || !muxIsOpen) {
      return;
    }

    // Don't create duplicate streams or recreate after completion
    if (streamRef.current || hasCompletedRef.current) {
      return;
    }

    let cancelled = false;

    const openStream = () => {
      if (cancelled) return;

      const stream = openDockerIndexerStream();
      if (!stream) {
        queueMicrotask(() => {
          setError("Failed to start indexer operation");
          setIsRunning(false);
        });
        return;
      }

      bindStream(stream);
    };

    openStream();

    return () => {
      cancelled = true;
    };
  }, [muxIsOpen, open, bindStream]);

  const handleClose = () => {
    if (isRunning) {
      closeStream();
    }
    onClose();
  };

  const getPhaseLabel = () => {
    switch (progress.phase) {
      case "connecting":
        return "Connecting to indexer...";
      case "indexing":
        return "Indexing Docker folder...";
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
      title="Indexing Docker Folder"
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
