import React, { useCallback, useEffect, useRef, useState } from "react";

import { openFileReindexStream, type Stream, useStreamMux } from "@/api";
import ReindexStatusDialog, {
  type ReindexStat,
} from "@/components/dialog/ReindexStatusDialog";
import { useStreamResult } from "@/hooks/useStreamResult";

interface ReindexDialogProps {
  open: boolean;
  onClose: () => void;
  path?: string;
  onComplete?: () => void;
  onRunningChange?: (isRunning: boolean) => void;
}

interface ReindexProgress {
  files_indexed: number;
  dirs_indexed: number;
  current_path?: string;
  phase?: string;
}

interface ReindexResult {
  path: string;
  files_indexed: number;
  dirs_indexed: number;
  total_size?: number;
  duration_ms: number;
}

const ReindexDialog: React.FC<ReindexDialogProps> = ({
  open,
  onClose,
  path = "/",
  onComplete,
  onRunningChange,
}) => {
  const [progress, setProgress] = useState<ReindexProgress>({
    files_indexed: 0,
    dirs_indexed: 0,
    phase: "connecting",
  });
  const [isRunning, setIsRunning] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);
  const [result, setResult] = useState<ReindexResult | null>(null);
  const streamRef = useRef<Stream | null>(null);
  const hasCompletedRef = useRef(false);
  const closedByUserRef = useRef(false);
  const { isOpen: muxIsOpen } = useStreamMux();
  const { run: runStreamResult } = useStreamResult();

  const closeStream = useCallback(() => {
    if (streamRef.current) {
      closedByUserRef.current = true;
      streamRef.current.close();
      streamRef.current = null;
    }
  }, []);

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

  useEffect(() => {
    if (!open) {
      closeStream();
    }
  }, [open, closeStream]);

  useEffect(() => {
    if (!open || !muxIsOpen) {
      return;
    }

    if (streamRef.current || hasCompletedRef.current) {
      return;
    }

    const stream = openFileReindexStream(path);
    if (!stream) {
      queueMicrotask(() => {
        setError("Failed to start reindex operation");
        setIsRunning(false);
      });
      return;
    }

    streamRef.current = stream;
    closedByUserRef.current = false;

    void runStreamResult<ReindexResult, ReindexProgress>({
      open: () => stream,
      onProgress: (progressData) => {
        setProgress(progressData);
      },
      closeMessage: "Reindex stream closed unexpectedly",
    })
      .then((reindexResult) => {
        hasCompletedRef.current = true;
        setResult(reindexResult);
        setSuccess(true);
        onComplete?.();
      })
      .catch((err: unknown) => {
        if (closedByUserRef.current) {
          return;
        }
        hasCompletedRef.current = true;
        const errorMessage =
          err instanceof Error ? err.message : "Reindex failed";
        setError(errorMessage);
      })
      .finally(() => {
        streamRef.current = null;
        setIsRunning(false);
      });
  }, [muxIsOpen, onComplete, open, path, runStreamResult]);

  useEffect(() => {
    onRunningChange?.(open && isRunning);
  }, [isRunning, onRunningChange, open]);

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
        return "Indexing filesystem...";
      default:
        return "Processing...";
    }
  };

  const progressStats: ReindexStat[] = [
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

  const successDescription = result
    ? `Indexed ${result.files_indexed.toLocaleString()} files and ${result.dirs_indexed.toLocaleString()} directories in ${(result.duration_ms / 1000).toFixed(2)}s`
    : undefined;

  return (
    <ReindexStatusDialog
      open={open}
      onClose={handleClose}
      onExited={resetState}
      title="Reindexing Filesystem"
      isRunning={isRunning}
      success={success}
      error={error}
      phaseLabel={getPhaseLabel()}
      progressStats={progressStats}
      showProgressStats={progress.phase === "indexing"}
      successDescription={successDescription}
    />
  );
};

export default ReindexDialog;
