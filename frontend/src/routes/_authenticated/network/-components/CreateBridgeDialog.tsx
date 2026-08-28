import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import { linuxio, type NetworkBridgeCandidate, useCallMutation } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppSelect from "@/components/ui/AppSelect";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { useScopedToast } from "@/hooks/useScopedToast";

const NETWORK_TOAST_META = {
  label: "Open network",
  to: "/network",
} as const;

const MAX_INTERFACE_NAME_LENGTH = 15;
const BRIDGE_NAME_PATTERN = /^[A-Za-z0-9_.-]+$/;

export const defaultBridgeName = (member: string): string =>
  `br-${member}`.slice(0, MAX_INTERFACE_NAME_LENGTH);

export const isBridgeNameValid = (name: string): boolean =>
  name.length > 0 &&
  name.length <= MAX_INTERFACE_NAME_LENGTH &&
  name !== "." &&
  name !== ".." &&
  BRIDGE_NAME_PATTERN.test(name);

interface CreateBridgeDialogProps {
  onClose: () => void;
  open: boolean;
}

const candidateReason = (candidate: NetworkBridgeCandidate): string => {
  const reasons = candidate.reasons?.filter(Boolean) ?? [];
  return reasons.length > 0 ? reasons.join("; ") : "Not eligible for Stage 2a";
};

const CreateBridgeDialog = ({ open, onClose }: CreateBridgeDialogProps) => {
  const toast = useScopedToast(NETWORK_TOAST_META);
  const [member, setMember] = useState("");
  const [bridgeName, setBridgeName] = useState("");

  // Candidate discovery is only needed while this workflow is visible. The
  // network page itself keeps its existing interface poll as its sole loader.
  const optionsQuery = useQuery({
    ...linuxio.network.get_bridge_options,
    enabled: open,
  });
  const options = optionsQuery.data;
  const candidates = options?.candidates ?? [];
  const eligibleCandidates = candidates.filter(
    (candidate) => candidate.eligible,
  );
  const selectedMember =
    eligibleCandidates.find((candidate) => candidate.name === member)?.name ??
    eligibleCandidates[0]?.name ??
    "";
  const effectiveBridgeName = bridgeName || defaultBridgeName(selectedMember);

  const { mutate: createBridge, isPending: isCreating } = useCallMutation(
    linuxio.network.create_bridge,
    {
      success: (result, request) => {
        toast.success(`Bridge "${result.name || request.name}" created`);
        setMember("");
        setBridgeName("");
        onClose();
      },
      error: "Failed to create bridge",
      toast: NETWORK_TOAST_META,
    },
  );

  const handleClose = () => {
    if (isCreating) return;
    setMember("");
    setBridgeName("");
    onClose();
  };

  const nameError =
    effectiveBridgeName.length > 0 && !isBridgeNameValid(effectiveBridgeName);
  const canCreate =
    open &&
    !optionsQuery.isPending &&
    !optionsQuery.isError &&
    !!selectedMember &&
    isBridgeNameValid(effectiveBridgeName) &&
    !isCreating;

  const handleMemberChange = (nextMember: string) => {
    setMember(nextMember);
    setBridgeName(defaultBridgeName(nextMember));
  };

  const handleCreate = () => {
    if (!canCreate) return;
    createBridge({ name: effectiveBridgeName, member: selectedMember });
  };

  return (
    <GeneralDialog
      aria-busy={isCreating}
      disableEscapeKeyDown={isCreating}
      fullWidth
      maxWidth="sm"
      onClose={handleClose}
      open={open}
    >
      <AppDialogTitle>Create host bridge</AppDialogTitle>
      <AppDialogContent>
        <div
          style={{
            display: "grid",
            gap: "var(--app-space-8)",
            marginTop: "var(--app-space-4)",
          }}
        >
          {optionsQuery.isPending && (
            <AppAlert severity="info">Checking spare wired NICs…</AppAlert>
          )}
          {optionsQuery.isError && (
            <AppAlert severity="error">
              Unable to inspect host networking. Try again after the network
              service is available.
            </AppAlert>
          )}
          {options?.warnings?.map((warning) => (
            <AppAlert key={warning} severity="warning">
              {warning}
            </AppAlert>
          ))}

          {candidates.length > 0 && (
            <AppSelect
              disabled={isCreating || eligibleCandidates.length === 0}
              fullWidth
              label="Spare wired NIC"
              onChange={(event) => handleMemberChange(event.target.value)}
              value={selectedMember}
            >
              {candidates.map((candidate) => (
                <option
                  disabled={!candidate.eligible}
                  key={candidate.name}
                  value={candidate.name}
                >
                  {candidate.name}
                  {!candidate.eligible
                    ? ` — ${candidateReason(candidate)}`
                    : ""}
                </option>
              ))}
            </AppSelect>
          )}

          {candidates.some((candidate) => !candidate.eligible) && (
            <div aria-label="Unavailable network interfaces" role="list">
              {candidates
                .filter((candidate) => !candidate.eligible)
                .map((candidate) => (
                  <AppTypography
                    color="text.secondary"
                    key={candidate.name}
                    role="listitem"
                    variant="caption"
                  >
                    {candidate.name}: {candidateReason(candidate)}
                    {candidate.backend
                      ? ` (managed by ${candidate.backend})`
                      : ""}
                  </AppTypography>
                ))}
            </div>
          )}

          {candidates.length > 0 && eligibleCandidates.length === 0 && (
            <AppAlert severity="warning">
              <AppAlertTitle>No eligible spare NICs</AppAlertTitle>
              Guided creation only accepts a wired NIC with no host IP
              configuration. Use NAT or an existing bridge for this host.
            </AppAlert>
          )}

          {!optionsQuery.isPending &&
            !optionsQuery.isError &&
            candidates.length === 0 && (
              <AppAlert severity="warning">
                <AppAlertTitle>No spare wired NICs found</AppAlertTitle>
                Stage 2a leaves the management interface unchanged and needs a
                second, unconfigured wired NIC.
              </AppAlert>
            )}

          {selectedMember && (
            <AppTextField
              disabled={isCreating}
              error={nameError}
              fullWidth
              helperText={
                nameError
                  ? `Use 1–${MAX_INTERFACE_NAME_LENGTH} letters, numbers, _, ., or -`
                  : `Linux interface names are limited to ${MAX_INTERFACE_NAME_LENGTH} characters`
              }
              label="Bridge name"
              onChange={(event) => setBridgeName(event.target.value)}
              value={effectiveBridgeName}
            />
          )}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isCreating} onClick={handleClose}>
          Cancel
        </AppButton>
        <AppButton
          disabled={!canCreate}
          onClick={handleCreate}
          variant="contained"
        >
          {isCreating ? "Creating…" : "Create bridge"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default CreateBridgeDialog;
