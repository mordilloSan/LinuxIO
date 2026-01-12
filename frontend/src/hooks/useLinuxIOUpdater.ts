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
  const updateStartedRef = useRef(false); // Track if update unit was successfully started
  const isUpdatingRef = useRef(false); // Ref to avoid stale closure in callbacks

  const startUpdate = useCallback(
    async (targetVersion?: string): Promise<void> => {
      const mux = getStreamMux();
      if (!mux || mux.status !== "open") {
        setError("Stream connection not ready");
        return;
      }

      setIsUpdating(true);
      isUpdatingRef.current = true; // Set ref immediately (no async delay)
      setProgress(0);
      setError(null);
      setStatus("Starting update...");
      setOutput([]);
      updateStartedRef.current = false;

      // Build command: download and execute install script as a detached systemd unit
      // We use systemd-run to spawn the update as a transient service, so it survives
      // when the main LinuxIO service is stopped during the update process.
      // After starting the unit, we follow its journal output until we lose connection.
      const updateCmd = targetVersion
        ? `curl -fsSL ${INSTALL_SCRIPT_URL} | bash -s -- ${targetVersion}`
        : `curl -fsSL ${INSTALL_SCRIPT_URL} | bash`;

      // Start update as transient systemd unit, then follow its journal output
      // --no-block: returns immediately after starting the unit
      // --unit: name the unit for easier tracking
      // -p StandardOutput=journal: send output to journal so we can stream it
      const cmd =
        `systemd-run --no-block --unit=linuxio-update ` +
        `-p StandardOutput=journal -p StandardError=journal ` +
        `bash -c '${updateCmd}' && ` +
        `sleep 0.5 && journalctl -f -u linuxio-update --no-pager -o cat`;

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

            // Detect when the systemd unit has been started or install script output is seen
            // This indicates the update is running in a detached unit
            const fullText = text.toLowerCase();
            if (
              fullText.includes("running as unit") ||
              fullText.includes("linuxio-update") ||
              fullText.includes("starting linuxio") ||
              fullText.includes("target version")
            ) {
              updateStartedRef.current = true;
            }
          }
        };

        stream.onResult = (result: ResultFrame) => {
          streamRef.current = null;

          if (result.status === "ok") {
            setProgress(100);
            setStatus("Update complete - service restarting");
            setIsUpdating(false);
            isUpdatingRef.current = false;
            resolve();
          } else {
            // If update was started (systemd unit is running), treat errors as expected
            // The error is likely from journalctl being killed when the service stops
            if (updateStartedRef.current) {
              setProgress(50);
              setStatus("Update in progress - service restarting...");
              setIsUpdating(false);
              isUpdatingRef.current = false;
              resolve(); // Update is running in background, wait for reconnection
            } else {
              const errorMsg = result.error || "Update failed";
              setError(errorMsg);
              setStatus("Update failed");
              setIsUpdating(false);
              isUpdatingRef.current = false;
              reject(new Error(errorMsg));
            }
          }
        };

        stream.onClose = () => {
          streamRef.current = null;
          // Connection loss during update is expected - the update runs in a separate
          // systemd unit and continues even when the main service is stopped.
          // The UpdateBanner component will handle reconnection and page reload.
          // Use ref instead of state to avoid stale closure issue
          if (isUpdatingRef.current) {
            if (updateStartedRef.current) {
              setProgress(50);
              setStatus("Update in progress - service restarting...");
              setIsUpdating(false);
              isUpdatingRef.current = false;
              resolve(); // Resolve so reconnection logic kicks in
            } else {
              setError("Connection lost before update started");
              setStatus("Connection lost");
              setIsUpdating(false);
              isUpdatingRef.current = false;
              reject(new Error("Stream closed before update started"));
            }
          }
        };
      });
    },
    [], // No deps needed - we use refs for values accessed in callbacks
  );

  const cancelUpdate = useCallback(() => {
    if (streamRef.current) {
      streamRef.current.abort();
      streamRef.current = null;
      setStatus("Update cancelled");
      setIsUpdating(false);
      isUpdatingRef.current = false;
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
