import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import { linuxio, type BtrfsReplicationJob, type BtrfsSSHTrust } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppSelect from "@/components/ui/AppSelect";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { useScopedToast } from "@/hooks/useScopedToast";
import { copyToClipboard } from "@/utils/clipboard";

import { BTRFS_TOAST_META } from "./btrfsShared";

const DEFAULT_SSH_PORT = 22;

interface BtrfsReplicationJobDialogProps {
  isSaving: boolean;
  job: BtrfsReplicationJob;
  onClose: () => void;
  onSave: (job: BtrfsReplicationJob) => void;
  sources: string[];
}

// The host the fingerprints were fetched for, so a stale result is never shown
// (or trusted) once the host or port is edited.
type HostKeyCheck = BtrfsSSHTrust & { host: string; port: number };

const BtrfsReplicationJobDialog = ({
  job,
  sources,
  isSaving,
  onSave,
  onClose,
}: BtrfsReplicationJobDialogProps) => {
  const toast = useScopedToast(BTRFS_TOAST_META);
  const [draft, setDraft] = useState(job);
  const [check, setCheck] = useState<HostKeyCheck | null>(null);

  const isSsh = draft.target.kind === "ssh";
  const host = draft.target.host?.trim() ?? "";
  const port = draft.target.port ?? DEFAULT_SSH_PORT;
  const user = draft.target.user?.trim() ?? "";
  const path = draft.target.path.trim();
  const isValid =
    draft.path !== "" &&
    path !== "" &&
    (!isSsh || (host !== "" && user !== "" && port > 0));
  const hostKey = check?.host === host && check.port === port ? check : null;

  const { data: sshKey } = useQuery({
    ...linuxio.storage.btrfs_replication_ssh_key,
    enabled: isSsh,
  });
  const { mutate: trustHost, isPending: isTrusting } =
    linuxio.storage.btrfs_replication_ssh_trust.useMutation({
      error: "Failed to check host key",
      toast: BTRFS_TOAST_META,
    });

  const setTarget = (patch: Partial<BtrfsReplicationJob["target"]>) =>
    setDraft({ ...draft, target: { ...draft.target, ...patch } });
  const handleHostKey = (confirm: boolean) =>
    trustHost(
      {
        host,
        port,
        confirm,
        ...(confirm && hostKey ? { fingerprints: hostKey.fingerprints } : {}),
      },
      { onSuccess: (result) => setCheck({ ...result, host, port }) },
    );
  const handleCopy = () =>
    copyToClipboard(sshKey?.publicKey ?? "").then(
      () => toast.success("Public key copied"),
      () => toast.error("Failed to copy public key"),
    );
  const handleSave = () =>
    // lastRun/lastError are server-owned and deliberately not sent.
    onSave({
      id: draft.id,
      uuid: draft.uuid,
      path: draft.path,
      target: isSsh
        ? { kind: "ssh", host, port, user, path }
        : { kind: "local", path },
      keepOnTarget: draft.keepOnTarget,
      enabled: draft.enabled,
      after: draft.after,
    });

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={onClose} open>
      <AppDialogTitle>Replication job</AppDialogTitle>
      <AppDialogContent>
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: "var(--app-space-4)",
          }}
        >
          <AppSelect
            fullWidth
            label="Source subvolume"
            onChange={(event) =>
              setDraft({ ...draft, path: event.target.value })
            }
            value={draft.path}
          >
            <option value="">Select a subvolume</option>
            {sources.map((source) => (
              <option key={source} value={source}>
                {source}
              </option>
            ))}
          </AppSelect>
          <AppSelect
            fullWidth
            label="Target kind"
            onChange={(event) =>
              setTarget({
                kind: event.target.value,
                port: draft.target.port ?? DEFAULT_SSH_PORT,
              })
            }
            value={draft.target.kind}
          >
            <option value="local">Local path</option>
            <option value="ssh">SSH</option>
          </AppSelect>
          {isSsh && (
            <>
              <AppTextField
                fullWidth
                label="Host"
                onChange={(event) => setTarget({ host: event.target.value })}
                size="small"
                value={draft.target.host ?? ""}
              />
              <AppTextField
                fullWidth
                label="Port"
                onChange={(event) =>
                  setTarget({ port: Math.max(0, Number(event.target.value)) })
                }
                size="small"
                type="number"
                value={port}
              />
              <AppTextField
                fullWidth
                label="User"
                onChange={(event) => setTarget({ user: event.target.value })}
                size="small"
                value={draft.target.user ?? ""}
              />
            </>
          )}
          <AppTextField
            fullWidth
            helperText="A btrfs subvolume dedicated to this job (created if missing); never shared with another job or host."
            label="Target path"
            onChange={(event) => setTarget({ path: event.target.value })}
            size="small"
            value={draft.target.path}
          />
          <AppTextField
            fullWidth
            helperText="0 keeps every replicated snapshot."
            label="Keep on target"
            onChange={(event) =>
              setDraft({
                ...draft,
                keepOnTarget: Math.max(0, Number(event.target.value) || 0),
              })
            }
            size="small"
            type="number"
            value={draft.keepOnTarget}
          />
          <AppSelect
            fullWidth
            label="Replicate"
            onChange={(event) =>
              setDraft({ ...draft, after: event.target.value })
            }
            value={draft.after}
          >
            <option value="timeline">After timeline snapshots</option>
            <option value="manual">Manual snapshots only</option>
          </AppSelect>
          <AppFormControlLabel
            control={
              <AppSwitch
                checked={draft.enabled}
                onChange={(_, enabled) => setDraft({ ...draft, enabled })}
              />
            }
            label="Enabled"
          />
          {isSsh && (
            <>
              <AppTextField
                endAdornment={
                  <AppButton
                    disabled={!sshKey}
                    onClick={handleCopy}
                    size="small"
                  >
                    Copy
                  </AppButton>
                }
                fullWidth
                helperText="Add this key to ~/.ssh/authorized_keys of the target user (root, or a user allowed to run btrfs receive)."
                label="Public key"
                readOnly
                size="small"
                value={sshKey?.publicKey ?? ""}
              />
              <div>
                <AppButton
                  disabled={host === "" || isTrusting}
                  onClick={() => handleHostKey(false)}
                  size="small"
                  variant="outlined"
                >
                  Check host key
                </AppButton>
              </div>
              {hostKey?.trusted && (
                <AppAlert severity="success">Host key trusted</AppAlert>
              )}
              {hostKey && !hostKey.trusted && (
                <AppAlert
                  action={
                    <AppButton
                      color="inherit"
                      disabled={isTrusting}
                      onClick={() => handleHostKey(true)}
                      size="small"
                    >
                      Trust
                    </AppButton>
                  }
                  severity="warning"
                >
                  <AppTypography variant="body2">
                    Host key fingerprints of {hostKey.host}:
                  </AppTypography>
                  {hostKey.fingerprints.map((fingerprint) => (
                    <div key={fingerprint}>{fingerprint}</div>
                  ))}
                </AppAlert>
              )}
            </>
          )}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isSaving} onClick={onClose}>
          Cancel
        </AppButton>
        <AppButton
          disabled={!isValid || isSaving}
          onClick={handleSave}
          variant="contained"
        >
          {isSaving ? "Saving..." : "Save"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default BtrfsReplicationJobDialog;
