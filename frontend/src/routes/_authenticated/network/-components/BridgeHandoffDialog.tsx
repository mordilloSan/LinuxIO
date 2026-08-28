import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import {
  LinuxIOError,
  isConnectionLossError,
  linuxio,
  type NetworkBridgeHandoffState,
  useCallMutation,
  useStreamMux,
} from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppCheckbox from "@/components/ui/AppCheckbox";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppSelect from "@/components/ui/AppSelect";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { useScopedToast } from "@/hooks/useScopedToast";

import { defaultBridgeName, isBridgeNameValid } from "./CreateBridgeDialog";

const NETWORK_TOAST_META = {
  label: "Open network",
  to: "/network",
} as const;

const EMPTY_OPERATION_ID = "00000000-0000-4000-8000-000000000000";

const TERMINAL_HANDOFF_STATES: ReadonlySet<NetworkBridgeHandoffState> = new Set(
  ["confirmed", "reverted", "unknown"],
);

const REVERTIBLE_HANDOFF_STATES: ReadonlySet<NetworkBridgeHandoffState> =
  new Set(["applying", "awaiting_confirmation", "unknown"]);

const isTerminalHandoffState = (
  state: NetworkBridgeHandoffState | undefined,
): boolean => Boolean(state && TERMINAL_HANDOFF_STATES.has(state));

const isRevertibleHandoffState = (
  state: NetworkBridgeHandoffState | undefined,
): boolean => Boolean(state && REVERTIBLE_HANDOFF_STATES.has(state));

const statusLabel = (state: NetworkBridgeHandoffState | undefined): string => {
  switch (state) {
    case "applying":
      return "Applying bridge configuration…";
    case "awaiting_confirmation":
      return "Connection restored — confirm the new bridge";
    case "confirmed":
      return "Bridge confirmed";
    case "reverted":
      return "Bridge handoff reverted";
    case "unknown":
      return "Waiting for the host to reconnect…";
    default:
      return "Ready to move the host IP";
  }
};

interface BridgeHandoffDialogProps {
  onClose: () => void;
  open: boolean;
}

const BridgeHandoffDialog = ({ open, onClose }: BridgeHandoffDialogProps) => {
  const toast = useScopedToast(NETWORK_TOAST_META);
  const { isOpen: muxIsOpen } = useStreamMux();
  const [member, setMember] = useState("");
  const [bridgeName, setBridgeName] = useState("");
  const [consoleAcknowledged, setConsoleAcknowledged] = useState(false);
  const [operationId, setOperationId] = useState("");
  const [transportLost, setTransportLost] = useState(false);

  const startMutation = useCallMutation(linuxio.network.start_bridge_handoff, {
    success: (result) => {
      setTransportLost(false);
      setOperationId(result.operationId);
    },
    error: (error, request) => {
      // Once a request stream was opened, a timeout or transport loss cannot
      // prove whether the host mutation started. Keep the client UUID and
      // resolve that ambiguity through the retry-safe status route.
      if (error.code === "outcome_unknown" || error.code === "timeout") {
        setOperationId(request.operationId);
        setTransportLost(true);
        return;
      }
      setOperationId("");
      setTransportLost(false);
      toast.error(error.message || "Unable to start the host bridge handoff");
    },
    toast: NETWORK_TOAST_META,
  });
  const confirmMutation = useCallMutation(
    linuxio.network.confirm_bridge_handoff,
    {
      success: (result) => {
        setTransportLost(false);
        if (result.operationId) setOperationId(result.operationId);
        toast.success("Host bridge confirmed");
      },
      error: "Unable to confirm the host bridge",
      toast: NETWORK_TOAST_META,
    },
  );
  const revertMutation = useCallMutation(
    linuxio.network.revert_bridge_handoff,
    {
      success: (result) => {
        setTransportLost(false);
        if (result.operationId) setOperationId(result.operationId);
        toast.success("Host bridge handoff reverted");
      },
      error: "Unable to revert the host bridge handoff",
      toast: NETWORK_TOAST_META,
    },
  );

  const interfacesQuery = useQuery({
    ...linuxio.network.get_network_info,
    enabled: open && !operationId,
  });
  const bridgeOptionsQuery = useQuery({
    ...linuxio.network.get_bridge_options,
    enabled: open && !operationId,
  });
  const interfaces = interfacesQuery.data ?? [];
  const candidates = (bridgeOptionsQuery.data?.candidates ?? []).filter(
    (candidate) => candidate.handoffEligible,
  );
  const selectedMember =
    candidates.find((candidate) => candidate.name === member)?.name ??
    candidates[0]?.name ??
    "";
  const handoffBlockers = (bridgeOptionsQuery.data?.candidates ?? []).flatMap(
    (candidate) =>
      (candidate.handoffReasons ?? []).map(
        (reason) => `${candidate.name}: ${reason}`,
      ),
  );
  const effectiveBridgeName = bridgeName || defaultBridgeName(selectedMember);

  // The stable placeholder keeps the descriptor well-typed before an
  // operation exists. Query execution remains disabled until the client has
  // an operation ID and the request transport is open again.
  const statusQuery = useQuery({
    ...linuxio.network.get_bridge_handoff({
      operationId: operationId || EMPTY_OPERATION_ID,
    }),
    enabled: open && Boolean(operationId) && muxIsOpen,
    refetchInterval: (query) =>
      open &&
      operationId &&
      muxIsOpen &&
      !isTerminalHandoffState(query.state.data?.state)
        ? 1000
        : false,
    refetchOnReconnect: true,
    retry: false,
    meta: { silent: true },
  });
  const status = statusQuery.data;
  const state = status?.state ?? (operationId ? "unknown" : undefined);
  const serverOutcomeUnknown = status?.state === "unknown";
  const terminal =
    isTerminalHandoffState(state) &&
    (state !== "unknown" || serverOutcomeUnknown);
  const pending =
    startMutation.isPending ||
    confirmMutation.isPending ||
    revertMutation.isPending;
  const statusTimedOut =
    statusQuery.error instanceof LinuxIOError &&
    statusQuery.error.code === "timeout";
  const statusNotFound =
    statusQuery.error instanceof LinuxIOError &&
    Number(statusQuery.error.code) === 404;
  const missingOperation =
    transportLost && statusNotFound && statusQuery.failureCount >= 2;
  const transientStatusError =
    Boolean(statusQuery.error) &&
    (isConnectionLossError(statusQuery.error) ||
      statusTimedOut ||
      (transportLost && statusNotFound));
  const active = Boolean(operationId) && !terminal && !missingOperation;
  const nameError =
    effectiveBridgeName.length > 0 && !isBridgeNameValid(effectiveBridgeName);
  const canStart =
    open &&
    !operationId &&
    !interfacesQuery.isPending &&
    !interfacesQuery.isError &&
    !bridgeOptionsQuery.isPending &&
    !bridgeOptionsQuery.isError &&
    Boolean(selectedMember) &&
    isBridgeNameValid(effectiveBridgeName) &&
    consoleAcknowledged &&
    !pending;

  const reset = () => {
    setMember("");
    setBridgeName("");
    setConsoleAcknowledged(false);
    setOperationId("");
    setTransportLost(false);
  };

  const handleClose = () => {
    if (pending || active) return;
    reset();
    onClose();
  };

  const handleMemberChange = (nextMember: string) => {
    setMember(nextMember);
    setBridgeName(defaultBridgeName(nextMember));
  };

  const handleStart = () => {
    if (!canStart) return;
    const nextOperationId = crypto.randomUUID();
    setTransportLost(false);
    startMutation.mutate({
      operationId: nextOperationId,
      name: effectiveBridgeName,
      member: selectedMember,
      consoleAcknowledged,
    });
  };

  const handleConfirm = () => {
    if (!operationId || state !== "awaiting_confirmation") return;
    confirmMutation.mutate({ operationId });
  };

  const handleRevert = () => {
    if (!operationId || !isRevertibleHandoffState(state)) return;
    revertMutation.mutate({ operationId });
  };

  const statusError =
    statusQuery.error && !transientStatusError
      ? statusQuery.error.message
      : null;

  return (
    <GeneralDialog
      aria-busy={pending}
      disableEscapeKeyDown={pending || active}
      fullWidth
      maxWidth="sm"
      onClose={handleClose}
      open={open}
    >
      <AppDialogTitle>Move host IP to bridge</AppDialogTitle>
      <AppDialogContent>
        <div
          style={{
            display: "grid",
            gap: "var(--app-space-8)",
            marginTop: "var(--app-space-4)",
          }}
        >
          {!operationId && (
            <>
              <AppAlert severity="warning">
                <AppAlertTitle>Single-NIC network risk</AppAlertTitle>
                The current management IP will move from the physical NIC to the
                bridge. Applying this can disconnect this page briefly. Keep the
                console open until the new bridge is confirmed. LinuxIO
                automatically restores the old configuration after about 90
                seconds without confirmation.
              </AppAlert>
              {(interfacesQuery.isPending || bridgeOptionsQuery.isPending) && (
                <AppAlert severity="info">Inspecting host interfaces…</AppAlert>
              )}
              {(interfacesQuery.isError || bridgeOptionsQuery.isError) && (
                <AppAlert severity="error">
                  Unable to inspect host networking. Try again when the network
                  service is available.
                </AppAlert>
              )}
              {!interfacesQuery.isPending &&
                !interfacesQuery.isError &&
                !bridgeOptionsQuery.isPending &&
                !bridgeOptionsQuery.isError &&
                candidates.length === 0 && (
                  <AppAlert severity="warning">
                    <AppAlertTitle>No eligible host-IP interface</AppAlertTitle>
                    {handoffBlockers.length > 0
                      ? handoffBlockers.join("; ")
                      : "No wired interface has safely restorable host network settings."}
                  </AppAlert>
                )}
              {candidates.length > 0 && (
                <AppSelect
                  disabled={pending}
                  fullWidth
                  label="Management interface"
                  onChange={(event) => handleMemberChange(event.target.value)}
                  value={selectedMember}
                >
                  {candidates.map((candidate) => {
                    const addresses =
                      interfaces.find((iface) => iface.name === candidate.name)
                        ?.ipv4 ?? [];
                    return (
                      <option key={candidate.name} value={candidate.name}>
                        {candidate.name}
                        {addresses.length > 0
                          ? ` — ${addresses.join(", ")}`
                          : ""}
                      </option>
                    );
                  })}
                </AppSelect>
              )}
              {selectedMember && (
                <AppTextField
                  disabled={pending}
                  error={nameError}
                  fullWidth
                  helperText={
                    nameError
                      ? "Use a valid Linux bridge name (up to 15 characters)"
                      : "The host IP will be moved to this bridge"
                  }
                  label="Bridge name"
                  onChange={(event) => setBridgeName(event.target.value)}
                  value={effectiveBridgeName}
                />
              )}
              <label
                style={{
                  alignItems: "flex-start",
                  display: "flex",
                  gap: "var(--app-space-4)",
                }}
              >
                <AppCheckbox
                  checked={consoleAcknowledged}
                  disabled={pending}
                  onChange={(_, checked) => setConsoleAcknowledged(checked)}
                />
                <AppTypography variant="body2">
                  I have console or other out-of-band access and understand that
                  this can temporarily disconnect the web UI.
                </AppTypography>
              </label>
            </>
          )}

          {operationId && (
            <AppAlert severity={statusError ? "error" : "info"}>
              <AppAlertTitle>
                {serverOutcomeUnknown
                  ? "Inspect the host bridge state"
                  : statusLabel(state)}
              </AppAlertTitle>
              {state === "unknown" &&
                !serverOutcomeUnknown &&
                !missingOperation && (
                  <AppDialogContentText>
                    Handoff status is not available yet. Leave this dialog open;
                    polling continues when the connection is available.
                  </AppDialogContentText>
                )}
              {missingOperation && (
                <AppDialogContentText>
                  No handoff record was created. It is safe to reset this dialog
                  and try again.
                </AppDialogContentText>
              )}
              {transientStatusError && !missingOperation && muxIsOpen && (
                <AppDialogContentText>
                  Status is temporarily unavailable. Polling will continue.
                </AppDialogContentText>
              )}
              {!muxIsOpen && active && (
                <AppDialogContentText>
                  Waiting for the LinuxIO connection…
                </AppDialogContentText>
              )}
              {statusError && (
                <AppDialogContentText>{statusError}</AppDialogContentText>
              )}
              {status?.message && (
                <AppDialogContentText>{status.message}</AppDialogContentText>
              )}
              {status?.error && (
                <AppDialogContentText>{status.error}</AppDialogContentText>
              )}
            </AppAlert>
          )}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={pending || active} onClick={handleClose}>
          Close
        </AppButton>
        {missingOperation && (
          <AppButton disabled={pending} onClick={reset} variant="outlined">
            Reset and retry
          </AppButton>
        )}
        {active && (
          <AppButton
            color="error"
            disabled={pending || !isRevertibleHandoffState(state)}
            onClick={handleRevert}
            variant="outlined"
          >
            {revertMutation.isPending ? "Reverting…" : "Revert"}
          </AppButton>
        )}
        {active && state === "awaiting_confirmation" && (
          <AppButton
            disabled={pending}
            onClick={handleConfirm}
            variant="contained"
          >
            {confirmMutation.isPending ? "Confirming…" : "Confirm bridge"}
          </AppButton>
        )}
        {!operationId && (
          <AppButton
            disabled={!canStart}
            onClick={handleStart}
            variant="contained"
          >
            {startMutation.isPending ? "Applying…" : "Move IP to bridge"}
          </AppButton>
        )}
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default BridgeHandoffDialog;
