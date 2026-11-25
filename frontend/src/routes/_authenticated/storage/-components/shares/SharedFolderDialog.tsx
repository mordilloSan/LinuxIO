import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import { getMutationErrorMessage, linuxio, type SharedFolder } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import TabSelector from "@/components/tabbar/TabSelector";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppCheckbox from "@/components/ui/AppCheckbox";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import useAuth from "@/hooks/useAuth";
import { useCapability } from "@/hooks/useCapabilities";

import { defaultRule } from "../btrfs/btrfsShared";
import AccessTable from "./AccessTable";
import NetworkOptions, {
  networkOf,
  networkProblem,
  timeMachineMaxBytes,
  type NetworkValue,
} from "./NetworkOptions";
import NFSOptionsDropdown, {
  defaultNFSOptions,
  nfsOptionsFromStrings,
  parseNFSClients,
  type ClientOptions,
} from "./NFSClientOptions";
import PoolFolderFields, {
  poolFolderError,
  sharePools,
  type PoolFolder,
} from "./PoolFolderFields";
import {
  accessLowered,
  accessProblem,
  defaultAccess,
  networkAddresses,
  SMB_RESTART_NOTE,
  timeMachineName,
  type AccessTable as AccessTableValue,
} from "./sharedFolders";

type DialogTab = "general" | "access" | "network";

const TABS: { label: string; value: DialogTab }[] = [
  { label: "General", value: "general" },
  { label: "Access", value: "access" },
  { label: "Network", value: "network" },
];

const emptyPoolFolder: PoolFolder = {
  uuid: "",
  name: "",
  limitGiB: "",
  snapshots: true,
};

const BYTES_PER_GIB = 1024 ** 3;

interface SharedFolderDialogProps {
  /** `null` creates a shared folder on a pool; a folder changes that one. */
  folder: SharedFolder | null;
  onClose: () => void;
  onExited?: () => void;
  open: boolean;
}

/**
 * Creates a shared folder, in one request the server undoes as a whole when
 * a step fails, or changes one: its share name, how it is shared and who may
 * use it. The caller mounts it afresh for each folder.
 */
const SharedFolderDialog = ({
  folder,
  onClose,
  onExited,
  open,
}: SharedFolderDialogProps) => {
  const { user } = useAuth();
  const samba = useCapability("sambaServerAvailable");
  const nfs = useCapability("nfsServerAvailable");
  const [tab, setTab] = useState<DialogTab>("general");
  const [poolFolder, setPoolFolder] = useState<PoolFolder>(emptyPoolFolder);
  const [shareName, setShareName] = useState(folder?.name ?? "");
  const [smbWanted, setSMBWanted] = useState(folder ? folder.smb : true);
  const [nfsWanted, setNFSWanted] = useState(Boolean(folder?.nfs.length));
  const [nfsClients, setNFSClients] = useState(
    () => folder?.nfs.map((client) => client.host).join(", ") || "*",
  );
  const [nfsOptions, setNFSOptions] = useState<ClientOptions>(() =>
    folder?.nfs[0]
      ? nfsOptionsFromStrings(folder.nfs[0].options ?? [])
      : { ...defaultNFSOptions },
  );
  // Null until it is edited: a new folder's table starts from the account
  // list, which arrives after the dialog opens.
  const [editedTable, setEditedTable] = useState<AccessTableValue | null>(
    folder ? { access: folder.access, everyone: folder.everyone } : null,
  );
  const [problem, setProblem] = useState<string | null>(null);
  // Null until it is edited: a new folder offers earlier versions when it
  // takes snapshots, as long as that is not set by hand.
  const [editedNetwork, setEditedNetwork] = useState<NetworkValue | null>(
    folder ? networkOf(folder) : null,
  );

  // A server that is not installed can still be switched off for a folder
  // that was shared through it.
  const smbOffered = samba.status !== "unavailable" || Boolean(folder?.smb);
  const nfsOffered =
    nfs.status !== "unavailable" || Boolean(folder?.nfs.length);
  const smb = smbWanted && smbOffered;
  const nfsOn = nfsWanted && nfsOffered;

  const { data: filesystems = [] } = useQuery({
    ...linuxio.storage.btrfs_list,
    enabled: open && !folder,
  });
  const usersQuery = useQuery({
    ...linuxio.accounts.list_users,
    enabled: open,
  });
  const users = usersQuery.data ?? [];
  const { data: groups = [] } = useQuery({
    ...linuxio.accounts.list_groups,
    enabled: open,
  });
  const { data: sambaUsers } = useQuery({
    ...linuxio.shares.list_samba_users,
    enabled: open && smb,
  });
  const pools = sharePools(filesystems);
  const table = editedTable ?? defaultAccess(user?.id, users);
  const lowered =
    folder !== null &&
    accessLowered({ access: folder.access, everyone: folder.everyone }, table);
  const name = folder ? shareName.trim() : poolFolder.name;
  const network = editedNetwork ?? {
    previousVersions: poolFolder.snapshots,
    recycleBin: true,
    timeMachine: false,
    timeMachineGiB: "",
  };
  // What Macs are told changes: the server restarts the SMB service.
  const restarts =
    timeMachineName({ name, smb, timeMachine: network.timeMachine }) !==
    (folder ? timeMachineName(folder) : "");

  const showError = (fallback: string) => (error: unknown) =>
    setProblem(getMutationErrorMessage(error, fallback));
  const create = linuxio.shares.create_folder.useMutation({
    success: "Shared folder created",
    error: showError("Failed to create the shared folder"),
    options: { onSuccess: onClose },
  });
  const update = linuxio.shares.update_folder.useMutation({
    success: "Shared folder saved",
    error: showError("Failed to save the shared folder"),
    options: { onSuccess: onClose },
  });
  const isPending = create.isPending || update.isPending;

  const handleSave = () => {
    const clients = nfsOn ? parseNFSClients(nfsClients, nfsOptions) : [];
    const accessIssue = accessProblem(table.access, table.everyone, smb);
    const invalid =
      (folder ? null : poolFolderError(poolFolder)) ??
      (name === "" ? "Give the share a name" : null) ??
      (nfsOn && clients.length === 0 ? "Name at least one NFS client" : null) ??
      networkProblem(network) ??
      accessIssue;
    if (invalid) {
      setProblem(invalid);
      // The table is fixed on its own tab.
      if (invalid === accessIssue) setTab("access");
      return;
    }
    setProblem(null);
    const offered = smb || folder !== null;
    const sharing = {
      ...table,
      name,
      smb,
      nfs: clients,
      // A new folder that is not shared over SMB starts with none of what
      // SMB offers; one that is saved keeps its switches for when it is
      // shared again.
      previousVersions: offered && network.previousVersions,
      recycleBin: offered && network.recycleBin,
      timeMachine: offered && network.timeMachine,
      timeMachineMaxBytes: offered ? timeMachineMaxBytes(network) : undefined,
    };
    if (folder) {
      update.mutate({ uuid: folder.uuid, path: folder.path, ...sharing });
      return;
    }
    const limit = poolFolder.limitGiB.trim();
    create.mutate({
      uuid: poolFolder.uuid,
      ...sharing,
      maxReferencedBytes:
        limit === "" ? undefined : Math.round(Number(limit) * BYTES_PER_GIB),
      snapshotRule: poolFolder.snapshots
        ? { ...defaultRule(poolFolder.uuid, name), enabled: true }
        : undefined,
    });
  };

  return (
    <GeneralDialog
      fullWidth
      maxWidth="sm"
      onClose={onClose}
      open={open}
      slotProps={{ transition: { onExited } }}
    >
      <AppDialogTitle>
        {folder ? folder.name : "Create shared folder"}
      </AppDialogTitle>
      <AppDialogContent>
        <div className="app-dialog-fields">
          {folder && (
            <AppTypography color="text.secondary" variant="body2">
              {folder.folder || "Its pool is not mounted"}
              {smb &&
                name !== "" &&
                ` · ${networkAddresses(window.location.hostname, name)}`}
            </AppTypography>
          )}
          <TabSelector onChange={setTab} options={TABS} value={tab} />
          {tab === "general" ? (
            <>
              {folder ? (
                <AppTextField
                  fullWidth
                  helperText="The name people connect to. The folder keeps its place on the pool."
                  label="Share name"
                  onChange={(event) => setShareName(event.target.value)}
                  size="small"
                  value={shareName}
                />
              ) : pools.length > 0 ? (
                <PoolFolderFields
                  folder={poolFolder}
                  onChange={setPoolFolder}
                  pools={pools}
                />
              ) : (
                <AppAlert severity="info">
                  A shared folder lives on a pool that is mounted. Create or
                  mount one first.
                </AppAlert>
              )}
              <AppFormControlLabel
                control={
                  <AppCheckbox
                    checked={smb}
                    onChange={(event) => setSMBWanted(event.target.checked)}
                  />
                }
                disabled={!smbOffered}
                label="Share with Windows and Mac (SMB)"
              />
              {samba.status === "unavailable" && (
                <AppAlert severity="warning">{samba.reason}</AppAlert>
              )}
              <AppFormControlLabel
                control={
                  <AppCheckbox
                    checked={nfsOn}
                    onChange={(event) => setNFSWanted(event.target.checked)}
                  />
                }
                disabled={!nfsOffered}
                label="Share with Linux and Unix (NFS)"
              />
              {nfs.status === "unavailable" && (
                <AppAlert severity="warning">{nfs.reason}</AppAlert>
              )}
              {nfsOn && (
                <div
                  style={{
                    display: "flex",
                    flexWrap: "wrap",
                    gap: "var(--app-space-8)",
                  }}
                >
                  <AppTextField
                    helperText="Hosts or networks, separated by commas; * is any."
                    label="NFS clients"
                    onChange={(event) => setNFSClients(event.target.value)}
                    size="small"
                    style={{ flex: 1, minWidth: 220 }}
                    value={nfsClients}
                  />
                  <NFSOptionsDropdown
                    onChange={setNFSOptions}
                    options={nfsOptions}
                  />
                </div>
              )}
              {nfsOn && (
                <AppTypography color="text.secondary" variant="caption">
                  NFS trusts the user IDs its clients send, so the access table
                  protects the folder only as well as those machines do.
                </AppTypography>
              )}
            </>
          ) : tab === "network" ? (
            <NetworkOptions
              onChange={setEditedNetwork}
              smb={smb}
              value={network}
            />
          ) : (
            <>
              <AccessTable
                currentUser={user?.id}
                groups={groups}
                onChange={setEditedTable}
                sambaUsers={smb ? sambaUsers : undefined}
                users={users}
                value={table}
              />
              {lowered && (
                <AppAlert severity="warning">
                  Over Windows and Mac, and on new files, the lower access
                  applies at once. Files and folders already inside keep the
                  access they were made with when opened on this server or over
                  NFS. No access applies everywhere at once.
                </AppAlert>
              )}
            </>
          )}
          {!folder && usersQuery.isError && (
            <AppAlert severity="error">
              The users and groups could not be read, so a new folder cannot be
              given its access yet. Close the dialog and try again.
            </AppAlert>
          )}
          {restarts && (
            <AppTypography color="text.secondary" variant="caption">
              {SMB_RESTART_NOTE}
            </AppTypography>
          )}
          {problem && <AppAlert severity="error">{problem}</AppAlert>}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={onClose}>Cancel</AppButton>
        <AppButton
          // A new folder's table starts from the account list: Create waits
          // for it, so it never starts from a guess.
          disabled={
            isPending ||
            (!folder && (pools.length === 0 || !usersQuery.isSuccess))
          }
          onClick={handleSave}
          variant="contained"
        >
          {folder ? "Save" : "Create"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default SharedFolderDialog;
