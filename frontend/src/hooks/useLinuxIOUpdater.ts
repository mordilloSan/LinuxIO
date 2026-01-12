import { useState, useCallback, useRef } from "react";

import {
  getStreamMux,
  Stream,
  ResultFrame,
  encodeString,
  decodeString,
} from "@/api/linuxio";

// Stream type for command execution (must match backend generic.StreamTypeExec)
const STREAM_TYPE_EXEC = "exec";

const INSTALL_SCRIPT_URL =
  "https://raw.githubusercontent.com/mordilloSan/LinuxIO/main/packaging/scripts/install-linuxio-binaries.sh";

export const useLinuxIOUpdater = () => {
  const [status, setStatus] = useState<string>("");
  const [progress, setProgress] = useState(0);
  const [output, setOutput] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [isUpdating, setIsUpdating] = useState(false);
  const streamRef = useRef<Stream | null>(null);

  const startUpdate = useCallback(
    async (targetVersion?: string): Promise<void> => {
      const mux = getStreamMux();
      if (!mux || mux.status !== "open") {
        setError("Stream connection not ready");
        return;
      }

      setIsUpdating(true);
      setProgress(0);
      setError(null);
      setStatus("Starting update...");
      setOutput([]);

      // Build command: download and execute install script
      // Format: bash -c "curl -fsSL <url> | bash -s -- <version>"
      const cmd = targetVersion
        ? `curl -fsSL ${INSTALL_SCRIPT_URL} | bash -s -- ${targetVersion}`
        : `curl -fsSL ${INSTALL_SCRIPT_URL} | bash`;

      // Build payload for exec stream: [stream_type, command, ...args]
      // We use bash -c to execute the full command as a single string
      const payloadParts = [STREAM_TYPE_EXEC, "bash", "-c", cmd];
      const payload = encodeString(payloadParts.join("\0"));

      const stream = mux.openStream(STREAM_TYPE_EXEC, payload);
      if (!stream) {
        setError("Failed to open update stream");
        setIsUpdating(false);
        return;
      }

      streamRef.current = stream;

      return new Promise<void>((resolve, reject) => {
        // Receive raw stdout/stderr as data
        stream.onData = (data: Uint8Array) => {
          const text = decodeString(data);

          // Split into lines and add to output
          const lines = text.split("\n").filter((line) => line.trim());
          if (lines.length > 0) {
            setOutput((prev) => [...prev, ...lines]);

            // Update status with last non-empty line
            const lastLine = lines[lines.length - 1];
            if (lastLine) {
              setStatus(lastLine);
            }
          }
        };

        stream.onResult = (result: ResultFrame) => {
          streamRef.current = null;

          if (result.status === "ok") {
            setProgress(100);
            setStatus("Update complete - service restarting");
            setIsUpdating(false);
            resolve();
          } else {
            const errorMsg = result.error || "Update failed";
            setError(errorMsg);
            setStatus("Update failed");
            setIsUpdating(false);
            reject(new Error(errorMsg));
          }
        };

        stream.onClose = () => {
          streamRef.current = null;
          // If closed without result while updating, treat as error
          if (isUpdating) {
            setError("Update stream closed unexpectedly");
            setStatus("Connection lost");
            setIsUpdating(false);
            reject(new Error("Stream closed unexpectedly"));
          }
        };
      });
    },
    [isUpdating],
  );

  const cancelUpdate = useCallback(() => {
    if (streamRef.current) {
      streamRef.current.abort();
      streamRef.current = null;
      setStatus("Update cancelled");
      setIsUpdating(false);
      setError("Update cancelled by user");
    }
  }, []);

  const clearError = useCallback(() => setError(null), []);

  return {
    startUpdate,
    cancelUpdate,
    clearError,
    status,
    progress,
    output,
    error,
    isUpdating,
  };
};
