import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useEffect, useState } from "react";

import {
  linuxio,
  type LogicalVolume,
  type PhysicalVolume,
  type VolumeGroup,
} from "@/api";
import LVMMetricCard from "@/components/cards/LVMMetricCard";
import LVMSectionCard from "@/components/cards/LVMSectionCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import PageLoader from "@/components/loaders/PageLoader";
import AppDataTable from "@/components/tables/AppDataTable";
import type { AppDataTableColumnDef } from "@/components/tables/AppDataTable";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppSelect from "@/components/ui/AppSelect";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { useScopedToast } from "@/hooks/useScopedToast";
import { GAP_SM } from "@/theme/constants";
import { formatFileSize } from "@/utils/formaters";
import { getMutationErrorMessage } from "@/utils/mutations";

interface LVMManagementProps {
  onMountCreateHandler?: (handler: () => void) => void;
}
interface CreateLVDialogProps {
  onClose: () => void;
  onSuccess: () => void;
  open: boolean;
  volumeGroups: VolumeGroup[];
}
interface ResizeLVDialogProps {
  lv: LogicalVolume | null;
  onClose: () => void;
  onSuccess: () => void;
  open: boolean;
}
interface DeleteLVDialogProps {
  lv: LogicalVolume | null;
  onClose: () => void;
  onSuccess: () => void;
  open: boolean;
}

type LVMSectionId = "lvs" | "vgs" | "pvs";

const PANEL_ACCENTS: Record<LVMSectionId, string> = {
  lvs: "var(--app-palette-primary-main)",
  vgs: "var(--app-palette-warning-main)",
  pvs: "var(--app-palette-success-main)",
};

const dialogStackStyle: React.CSSProperties = {
  display: "flex",
  flexDirection: "column",
  gap: 12,
  marginTop: 4,
};

const monospaceStyle: React.CSSProperties = {
  fontFamily: "monospace",
};

const getUsageColor = (usedPct: number): "primary" | "warning" | "error" => {
  if (usedPct > 90) return "error";
  if (usedPct > 70) return "warning";
  return "primary";
};

const CreateLVDialog: React.FC<CreateLVDialogProps> = ({
  open,
  onClose,
  volumeGroups,
  onSuccess,
}) => {
  const queryClient = useQueryClient();
  const toast = useScopedToast({ href: "/storage", label: "Open storage" });
  const [vgName, setVgName] = useState("");
  const [lvName, setLvName] = useState("");
  const [size, setSize] = useState("");
  const [validationError, setValidationError] = useState<string | null>(null);
  const { mutate: createLV, isPending: isCreating } =
    linuxio.storage.create_lv.useMutation({
      onSuccess: () => {
        toast.success(`Logical volume ${lvName} created successfully`);
        queryClient.invalidateQueries({
          queryKey: linuxio.storage.list_lvs.queryKey(),
        });
        queryClient.invalidateQueries({
          queryKey: linuxio.storage.list_vgs.queryKey(),
        });
        onSuccess();
        handleClose();
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to create logical volume"),
        );
      },
    });
  const handleCreate = () => {
    if (!vgName || !lvName || !size) {
      setValidationError("All fields are required");
      return;
    }
    setValidationError(null);
    createLV({ vgName, lvName, size });
  };
  const handleClose = () => {
    setVgName("");
    setLvName("");
    setSize("");
    setValidationError(null);
    onClose();
  };
  const selectedVG = volumeGroups.find((vg) => vg.name === vgName);
  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={handleClose} open={open}>
      <AppDialogTitle>Create Logical Volume</AppDialogTitle>
      <AppDialogContent>
        <div style={dialogStackStyle}>
          <AppSelect
            disabled={volumeGroups.length === 0}
            fullWidth
            label="Volume Group"
            onChange={(e) => setVgName(e.target.value)}
            value={vgName}
          >
            <option disabled value="">
              Select a volume group
            </option>
            {volumeGroups.map((vg) => (
              <option key={vg.name} value={vg.name}>
                {vg.name} ({formatFileSize(vg.free)} free)
              </option>
            ))}
          </AppSelect>
          {volumeGroups.length === 0 && (
            <AppAlert severity="info">
              No volume groups are available yet. Create one before provisioning
              a logical volume.
            </AppAlert>
          )}
          {selectedVG && (
            <div
              style={{
                padding: 10,
                borderRadius: 12,
                background: "var(--app-palette-action-hover)",
                border:
                  "1px solid color-mix(in srgb, currentColor 12%, transparent)",
              }}
            >
              <AppTypography color="text.secondary" variant="caption">
                Available space in {selectedVG.name}
              </AppTypography>
              <AppTypography fontWeight={700} variant="body2">
                {formatFileSize(selectedVG.free)}
              </AppTypography>
            </div>
          )}
          <AppTextField
            fullWidth
            label="Logical Volume Name"
            onChange={(e) => setLvName(e.target.value)}
            placeholder="e.g., data, backup"
            size="small"
            value={lvName}
          />
          <AppTextField
            fullWidth
            helperText="Use K, M, G, T suffix for size units"
            label="Size"
            onChange={(e) => setSize(e.target.value)}
            placeholder="e.g., 10G, 500M"
            size="small"
            value={size}
          />
          {validationError && (
            <AppAlert severity="error">{validationError}</AppAlert>
          )}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isCreating} onClick={handleClose}>
          Cancel
        </AppButton>
        <AppButton
          disabled={isCreating || volumeGroups.length === 0}
          onClick={handleCreate}
          variant="contained"
        >
          {isCreating ? "Creating..." : "Create"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};
const ResizeLVDialog: React.FC<ResizeLVDialogProps> = ({
  open,
  onClose,
  lv,
  onSuccess,
}) => {
  const queryClient = useQueryClient();
  const toast = useScopedToast({ href: "/storage", label: "Open storage" });
  const [newSize, setNewSize] = useState(() =>
    lv ? `${Math.round(lv.size / (1024 * 1024 * 1024))}G` : "",
  );
  const [validationError, setValidationError] = useState<string | null>(null);
  const { mutate: resizeLV, isPending: isResizing } =
    linuxio.storage.resize_lv.useMutation({
      onSuccess: () => {
        toast.success(`Logical volume ${lv?.name} resized successfully`);
        queryClient.invalidateQueries({
          queryKey: linuxio.storage.list_lvs.queryKey(),
        });
        queryClient.invalidateQueries({
          queryKey: linuxio.storage.list_vgs.queryKey(),
        });
        onSuccess();
        handleClose();
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to resize logical volume"),
        );
      },
    });
  const handleResize = () => {
    if (!lv || !newSize) {
      setValidationError("Size is required");
      return;
    }
    setValidationError(null);
    resizeLV({ vgName: lv.vgName, lvName: lv.name, newSize });
  };
  const handleClose = () => {
    setNewSize("");
    setValidationError(null);
    onClose();
  };
  return (
    <GeneralDialog
      fullWidth
      key={lv?.path}
      maxWidth="sm"
      onClose={handleClose}
      open={open}
    >
      <AppDialogTitle>Resize Logical Volume</AppDialogTitle>
      <AppDialogContent>
        <div style={dialogStackStyle}>
          {lv && (
            <div
              style={{
                padding: 10,
                borderRadius: 12,
                background: "var(--app-palette-action-hover)",
                border:
                  "1px solid color-mix(in srgb, currentColor 12%, transparent)",
                display: "grid",
                gap: 4,
              }}
            >
              <AppTypography color="text.secondary" variant="caption">
                Selected volume
              </AppTypography>
              <AppTypography fontWeight={700} variant="body2">
                {lv.name}
              </AppTypography>
              <AppTypography color="text.secondary" variant="body2">
                {lv.vgName} · {formatFileSize(lv.size)}
              </AppTypography>
              <AppTypography color="text.secondary" variant="caption">
                {lv.path}
              </AppTypography>
            </div>
          )}
          <AppTextField
            fullWidth
            helperText="Use K, M, G, T suffix for size units"
            label="New Size"
            onChange={(e) => setNewSize(e.target.value)}
            placeholder="e.g., 20G, 1T"
            size="small"
            value={newSize}
          />
          {validationError && (
            <AppAlert severity="error">{validationError}</AppAlert>
          )}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isResizing} onClick={handleClose}>
          Cancel
        </AppButton>
        <AppButton
          disabled={isResizing}
          onClick={handleResize}
          variant="contained"
        >
          {isResizing ? "Resizing..." : "Resize"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};
const DeleteLVDialog: React.FC<DeleteLVDialogProps> = ({
  open,
  onClose,
  lv,
  onSuccess,
}) => {
  const queryClient = useQueryClient();
  const toast = useScopedToast({ href: "/storage", label: "Open storage" });
  const { mutate: deleteLV, isPending: isDeleting } =
    linuxio.storage.delete_lv.useMutation({
      onSuccess: () => {
        toast.success(`Logical volume ${lv?.name} deleted successfully`);
        queryClient.invalidateQueries({
          queryKey: linuxio.storage.list_lvs.queryKey(),
        });
        queryClient.invalidateQueries({
          queryKey: linuxio.storage.list_vgs.queryKey(),
        });
        onSuccess();
        handleClose();
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to delete logical volume"),
        );
      },
    });
  const handleDelete = () => {
    if (!lv) return;
    deleteLV({ vgName: lv.vgName, lvName: lv.name });
  };
  const handleClose = () => {
    onClose();
  };
  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={handleClose} open={open}>
      <AppDialogTitle>Delete Logical Volume</AppDialogTitle>
      <AppDialogContent>
        <AppDialogContentText>
          Are you sure you want to delete the logical volume{" "}
          <strong>{lv?.name}</strong>?
        </AppDialogContentText>
        {lv?.mountpoint && (
          <AppAlert
            severity="warning"
            style={{
              marginTop: 8,
            }}
          >
            This volume is currently mounted at <strong>{lv.mountpoint}</strong>
            . Please unmount it first.
          </AppAlert>
        )}
        <AppAlert
          severity="error"
          style={{
            marginTop: 8,
          }}
        >
          This action cannot be undone. All data on this volume will be lost.
        </AppAlert>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isDeleting} onClick={handleClose}>
          Cancel
        </AppButton>
        <AppButton
          color="error"
          disabled={isDeleting || !!lv?.mountpoint}
          onClick={handleDelete}
          variant="contained"
        >
          {isDeleting ? "Deleting..." : "Delete"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};
const PVTable: React.FC<{
  data: PhysicalVolume[];
}> = ({ data }) => {
  const columns: AppDataTableColumnDef<PhysicalVolume>[] = [
    {
      accessorKey: "name",
      header: "Name",
      cell: ({ row }) => (
        <AppTypography style={monospaceStyle} variant="body2">
          {row.original.name}
        </AppTypography>
      ),
    },
    {
      accessorKey: "vgName",
      header: "Volume Group",
      cell: ({ row }) => row.original.vgName || "-",
    },
    {
      accessorKey: "size",
      header: "Size",
      cell: ({ row }) => formatFileSize(row.original.size),
    },
    {
      accessorKey: "free",
      header: "Free",
      cell: ({ row }) => formatFileSize(row.original.free),
    },
    {
      accessorKey: "format",
      header: "Format",
      cell: ({ row }) => (
        <Chip label={row.original.format} size="small" variant="soft" />
      ),
    },
  ];

  return (
    <AppDataTable
      ariaLabel="LVM physical volumes"
      columns={columns}
      data={data}
      density="compact"
      emptyMessage="No physical volumes found"
      getRowId={(pv) => pv.name}
      maxHeight={320}
      variant="embedded"
    />
  );
};
const VGTable: React.FC<{
  data: VolumeGroup[];
}> = ({ data }) => {
  const columns: AppDataTableColumnDef<VolumeGroup>[] = [
    {
      accessorKey: "name",
      header: "Name",
      cell: ({ row }) => (
        <AppTypography fontWeight={600} variant="body2">
          {row.original.name}
        </AppTypography>
      ),
    },
    {
      accessorKey: "size",
      header: "Size",
      cell: ({ row }) => formatFileSize(row.original.size),
    },
    {
      accessorKey: "free",
      header: "Free",
      cell: ({ row }) => formatFileSize(row.original.free),
    },
    {
      accessorKey: "pvCount",
      header: "PVs",
    },
    {
      accessorKey: "lvCount",
      header: "LVs",
    },
  ];

  return (
    <AppDataTable
      ariaLabel="LVM volume groups"
      columns={columns}
      data={data}
      density="compact"
      emptyMessage="No volume groups found"
      getRowId={(vg) => vg.name}
      maxHeight={320}
      variant="embedded"
    />
  );
};
interface LVTableProps {
  data: LogicalVolume[];
  onDelete: (lv: LogicalVolume) => void;
  onResize: (lv: LogicalVolume) => void;
}
const LVTable: React.FC<LVTableProps> = ({ data, onResize, onDelete }) => (
  <AppDataTable
    ariaLabel="LVM logical volumes"
    columns={[
      {
        accessorKey: "name",
        header: "Name",
        cell: ({ row }) => (
          <div>
            <AppTypography fontWeight={600} variant="body2">
              {row.original.name}
            </AppTypography>
            <AppTypography
              color="text.secondary"
              style={monospaceStyle}
              variant="caption"
            >
              {row.original.path}
            </AppTypography>
          </div>
        ),
      },
      {
        accessorKey: "vgName",
        header: "Volume Group",
      },
      {
        accessorKey: "size",
        header: "Size",
        cell: ({ row }) => formatFileSize(row.original.size),
      },
      {
        accessorKey: "mountpoint",
        header: "Mountpoint",
        cell: ({ row }) =>
          row.original.mountpoint ? (
            <AppTypography style={monospaceStyle} variant="body2">
              {row.original.mountpoint}
            </AppTypography>
          ) : (
            <Chip label="Not mounted" size="small" variant="soft" />
          ),
      },
      {
        accessorKey: "usedPct",
        header: "Usage",
        cell: ({ row }) =>
          row.original.mountpoint ? (
            <div style={{ width: 100 }}>
              <AppLinearProgress
                color={getUsageColor(row.original.usedPct)}
                style={{
                  borderRadius: 3,
                  height: 6,
                  marginBottom: 2,
                }}
                value={row.original.usedPct}
                variant="determinate"
              />
              <AppTypography variant="caption">
                {row.original.usedPct.toFixed(1)}%
              </AppTypography>
            </div>
          ) : (
            "-"
          ),
      },
      {
        id: "actions",
        header: "Actions",
        cell: ({ row }) => (
          <div
            style={{
              display: "flex",
              flexWrap: "wrap",
              gap: 8,
              justifyContent: "flex-end",
            }}
          >
            <AppButton
              onClick={() => onResize(row.original)}
              size="small"
              startIcon={<Icon height={18} icon="mdi:pencil" width={18} />}
              variant="outlined"
            >
              Resize
            </AppButton>
            <AppButton
              color="error"
              onClick={() => onDelete(row.original)}
              size="small"
              startIcon={<Icon height={18} icon="mdi:delete" width={18} />}
              variant="outlined"
            >
              Delete
            </AppButton>
          </div>
        ),
        meta: { align: "right", width: "minmax(180px, 220px)" },
      },
    ]}
    data={data}
    density="compact"
    emptyMessage="No logical volumes found"
    getRowId={(lv) => lv.path}
    maxHeight={360}
    variant="embedded"
  />
);
const LVMManagement: React.FC<LVMManagementProps> = ({
  onMountCreateHandler,
}) => {
  const [expanded, setExpanded] = useState<LVMSectionId | false>("lvs");
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [resizeDialogOpen, setResizeDialogOpen] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [selectedLV, setSelectedLV] = useState<LogicalVolume | null>(null);
  const {
    data: pvs = [],
    isPending: pvsLoading,
    refetch: refetchPVs,
  } = linuxio.storage.list_pvs.useQuery({
    refetchInterval: 10000,
  });
  const {
    data: vgs = [],
    isPending: vgsLoading,
    refetch: refetchVGs,
  } = linuxio.storage.list_vgs.useQuery({
    refetchInterval: 10000,
  });
  const {
    data: lvs = [],
    isPending: lvsLoading,
    refetch: refetchLVs,
  } = linuxio.storage.list_lvs.useQuery({
    refetchInterval: 10000,
  });
  const handleCreateLV = useCallback(() => {
    setCreateDialogOpen(true);
  }, []);
  useEffect(() => {
    if (onMountCreateHandler) {
      onMountCreateHandler(handleCreateLV);
    }
  }, [onMountCreateHandler, handleCreateLV]);
  const handleSectionToggle = (panel: LVMSectionId) => {
    setExpanded((current) => (current === panel ? false : panel));
  };
  const handleRefreshAll = () => {
    refetchPVs();
    refetchVGs();
    refetchLVs();
  };
  const handleResize = (lv: LogicalVolume) => {
    setSelectedLV(lv);
    setResizeDialogOpen(true);
  };
  const handleDelete = (lv: LogicalVolume) => {
    setSelectedLV(lv);
    setDeleteDialogOpen(true);
  };
  if (pvsLoading || vgsLoading || lvsLoading) {
    return <PageLoader />;
  }
  const pvsList = Array.isArray(pvs) ? pvs : [];
  const vgsList = Array.isArray(vgs) ? vgs : [];
  const lvsList = Array.isArray(lvs) ? lvs : [];
  const mountedLvCount = lvsList.filter((lv) => !!lv.mountpoint).length;
  const totalLvCapacity = lvsList.reduce((sum, lv) => sum + lv.size, 0);
  const totalVgFree = vgsList.reduce((sum, vg) => sum + vg.free, 0);
  const totalPvCapacity = pvsList.reduce((sum, pv) => sum + pv.size, 0);
  return (
    <>
      <div
        style={{
          display: "grid",
          gap: GAP_SM,
        }}
      >
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))",
            gap: GAP_SM,
          }}
        >
          <LVMMetricCard
            color={PANEL_ACCENTS.lvs}
            icon="mdi:database-outline"
            subtitle={`Provisioned ${formatFileSize(totalLvCapacity)}`}
            title="Logical Volumes"
            value={String(lvsList.length)}
          />
          <LVMMetricCard
            color={PANEL_ACCENTS.vgs}
            icon="mdi:layers-triple-outline"
            subtitle={`${formatFileSize(totalVgFree)} free capacity`}
            title="Volume Groups"
            value={String(vgsList.length)}
          />
          <LVMMetricCard
            color={PANEL_ACCENTS.pvs}
            icon="mdi:harddisk"
            subtitle={`${formatFileSize(totalPvCapacity)} raw capacity`}
            title="Physical Volumes"
            value={String(pvsList.length)}
          />
        </div>

        <LVMSectionCard
          accent={PANEL_ACCENTS.lvs}
          count={lvsList.length}
          expanded={expanded === "lvs"}
          icon="mdi:database-outline"
          onToggle={() => handleSectionToggle("lvs")}
          subtitle={`${mountedLvCount} mounted across ${vgsList.length} volume group${vgsList.length === 1 ? "" : "s"}`}
          title="Logical Volumes"
        >
          <LVTable
            data={lvsList}
            onDelete={handleDelete}
            onResize={handleResize}
          />
        </LVMSectionCard>

        <LVMSectionCard
          accent={PANEL_ACCENTS.vgs}
          count={vgsList.length}
          expanded={expanded === "vgs"}
          icon="mdi:layers-triple-outline"
          onToggle={() => handleSectionToggle("vgs")}
          subtitle={`${formatFileSize(totalVgFree)} free across all groups`}
          title="Volume Groups"
        >
          <VGTable data={vgsList} />
        </LVMSectionCard>

        <LVMSectionCard
          accent={PANEL_ACCENTS.pvs}
          count={pvsList.length}
          expanded={expanded === "pvs"}
          icon="mdi:harddisk"
          onToggle={() => handleSectionToggle("pvs")}
          subtitle={`${formatFileSize(totalPvCapacity)} discovered device capacity`}
          title="Physical Volumes"
        >
          <PVTable data={pvsList} />
        </LVMSectionCard>
      </div>

      <CreateLVDialog
        onClose={() => setCreateDialogOpen(false)}
        onSuccess={handleRefreshAll}
        open={createDialogOpen}
        volumeGroups={vgsList}
      />

      <ResizeLVDialog
        key={selectedLV?.name ?? ""}
        lv={selectedLV}
        onClose={() => setResizeDialogOpen(false)}
        onSuccess={handleRefreshAll}
        open={resizeDialogOpen}
      />

      <DeleteLVDialog
        lv={selectedLV}
        onClose={() => setDeleteDialogOpen(false)}
        onSuccess={handleRefreshAll}
        open={deleteDialogOpen}
      />
    </>
  );
};
export default LVMManagement;
