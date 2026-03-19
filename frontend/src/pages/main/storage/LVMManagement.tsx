import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";

import {
  linuxio,
  type LogicalVolume,
  type PhysicalVolume,
  type VolumeGroup,
} from "@/api";
import FrostedCard from "@/components/cards/RootCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import ComponentLoader from "@/components/loaders/ComponentLoader";
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
import {
  AppTable,
  AppTableBody,
  AppTableCell,
  AppTableContainer,
  AppTableHead,
  AppTableRow,
} from "@/components/ui/AppTable";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { formatFileSize } from "@/utils/formaters";
import { getMutationErrorMessage } from "@/utils/mutations";

interface LVMManagementProps {
  onMountCreateHandler?: (handler: () => void) => void;
}
interface CreateLVDialogProps {
  open: boolean;
  onClose: () => void;
  volumeGroups: VolumeGroup[];
  onSuccess: () => void;
}
interface ResizeLVDialogProps {
  open: boolean;
  onClose: () => void;
  lv: LogicalVolume | null;
  onSuccess: () => void;
}
interface DeleteLVDialogProps {
  open: boolean;
  onClose: () => void;
  lv: LogicalVolume | null;
  onSuccess: () => void;
}

type LVMSectionId = "lvs" | "vgs" | "pvs";

const PANEL_ACCENTS: Record<LVMSectionId, string> = {
  lvs: "var(--mui-palette-primary-main)",
  vgs: "var(--mui-palette-warning-main)",
  pvs: "var(--mui-palette-success-main)",
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

interface LVMMetricCardProps {
  title: string;
  value: string;
  subtitle: string;
  icon: string;
  color: string;
}

const LVMMetricCard: React.FC<LVMMetricCardProps> = ({
  title,
  value,
  subtitle,
  icon,
  color,
}) => (
  <FrostedCard
    style={{
      padding: 14,
      minWidth: 0,
    }}
  >
    <div
      style={{
        display: "flex",
        alignItems: "flex-start",
        justifyContent: "space-between",
        gap: 12,
      }}
    >
      <div style={{ minWidth: 0 }}>
        <AppTypography
          variant="caption"
          color="text.secondary"
          style={{
            textTransform: "uppercase",
            letterSpacing: "0.08em",
          }}
        >
          {title}
        </AppTypography>
        <AppTypography
          variant="subtitle1"
          fontWeight={800}
          style={{
            marginTop: 4,
            marginBottom: 4,
          }}
        >
          {value}
        </AppTypography>
        <AppTypography variant="body2" color="text.secondary">
          {subtitle}
        </AppTypography>
      </div>
      <div
        style={{
          width: 42,
          height: 42,
          borderRadius: 14,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          color,
          background: `color-mix(in srgb, ${color} 16%, transparent)`,
          border: `1px solid color-mix(in srgb, ${color} 30%, transparent)`,
          flexShrink: 0,
        }}
      >
        <Icon icon={icon} width={22} height={22} />
      </div>
    </div>
  </FrostedCard>
);

interface LVMSectionCardProps {
  title: string;
  subtitle: string;
  count: number;
  icon: string;
  accent: string;
  expanded: boolean;
  onToggle: () => void;
  children: React.ReactNode;
}

const LVMSectionCard: React.FC<LVMSectionCardProps> = ({
  title,
  subtitle,
  count,
  icon,
  accent,
  expanded,
  onToggle,
  children,
}) => (
  <FrostedCard style={{ padding: 12 }}>
    <div
      role="button"
      tabIndex={0}
      onClick={onToggle}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onToggle();
        }
      }}
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        gap: 12,
        cursor: "pointer",
        userSelect: "none",
      }}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 12,
          minWidth: 0,
        }}
      >
        <div
          style={{
            width: 46,
            height: 46,
            borderRadius: 14,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            color: accent,
            background: `color-mix(in srgb, ${accent} 16%, transparent)`,
            border: `1px solid color-mix(in srgb, ${accent} 30%, transparent)`,
            flexShrink: 0,
          }}
        >
          <Icon icon={icon} width={24} height={24} />
        </div>
        <div style={{ minWidth: 0 }}>
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 8,
              flexWrap: "wrap",
              marginBottom: 2,
            }}
          >
            <AppTypography variant="subtitle1" fontWeight={700}>
              {title}
            </AppTypography>
            <Chip label={`${count}`} size="small" variant="soft" />
          </div>
          <AppTypography variant="body2" color="text.secondary">
            {subtitle}
          </AppTypography>
        </div>
      </div>
      <div
        style={{
          width: 36,
          height: 36,
          borderRadius: 12,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          background: "var(--mui-palette-action-hover)",
          color: "var(--mui-palette-text-secondary)",
          flexShrink: 0,
        }}
      >
        <Icon
          icon="mdi:chevron-down"
          width={22}
          height={22}
          style={{
            transform: expanded ? "rotate(0deg)" : "rotate(-90deg)",
            transition: "transform 0.2s ease",
          }}
        />
      </div>
    </div>
    {expanded ? <div style={{ marginTop: 14 }}>{children}</div> : null}
  </FrostedCard>
);
const CreateLVDialog: React.FC<CreateLVDialogProps> = ({
  open,
  onClose,
  volumeGroups,
  onSuccess,
}) => {
  const queryClient = useQueryClient();
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
    createLV([vgName, lvName, size]);
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
    <GeneralDialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
      <AppDialogTitle>Create Logical Volume</AppDialogTitle>
      <AppDialogContent>
        <div style={dialogStackStyle}>
          <AppSelect
            label="Volume Group"
            fullWidth
            value={vgName}
            onChange={(e) => setVgName(e.target.value)}
            disabled={volumeGroups.length === 0}
          >
            <option value="" disabled>
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
                background: "var(--mui-palette-action-hover)",
                border:
                  "1px solid color-mix(in srgb, currentColor 12%, transparent)",
              }}
            >
              <AppTypography variant="caption" color="text.secondary">
                Available space in {selectedVG.name}
              </AppTypography>
              <AppTypography variant="body2" fontWeight={700}>
                {formatFileSize(selectedVG.free)}
              </AppTypography>
            </div>
          )}
          <AppTextField
            label="Logical Volume Name"
            value={lvName}
            onChange={(e) => setLvName(e.target.value)}
            placeholder="e.g., data, backup"
            fullWidth
            size="small"
          />
          <AppTextField
            label="Size"
            value={size}
            onChange={(e) => setSize(e.target.value)}
            placeholder="e.g., 10G, 500M"
            helperText="Use K, M, G, T suffix for size units"
            fullWidth
            size="small"
          />
          {validationError && (
            <AppAlert severity="error">{validationError}</AppAlert>
          )}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={handleClose} disabled={isCreating}>
          Cancel
        </AppButton>
        <AppButton
          onClick={handleCreate}
          variant="contained"
          disabled={isCreating || volumeGroups.length === 0}
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
  const [newSize, setNewSize] = useState("");
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
  useEffect(() => {
    if (!lv) {
      setNewSize("");
      setValidationError(null);
      return;
    }
    const sizeGB = Math.round(lv.size / (1024 * 1024 * 1024));
    setNewSize(`${sizeGB}G`);
    setValidationError(null);
  }, [lv]);
  const handleResize = () => {
    if (!lv || !newSize) {
      setValidationError("Size is required");
      return;
    }
    setValidationError(null);
    resizeLV([lv.vgName, lv.name, newSize]);
  };
  const handleClose = () => {
    setNewSize("");
    setValidationError(null);
    onClose();
  };
  return (
    <GeneralDialog
      key={lv?.path}
      open={open}
      onClose={handleClose}
      maxWidth="sm"
      fullWidth
    >
      <AppDialogTitle>Resize Logical Volume</AppDialogTitle>
      <AppDialogContent>
        <div style={dialogStackStyle}>
          {lv && (
            <div
              style={{
                padding: 10,
                borderRadius: 12,
                background: "var(--mui-palette-action-hover)",
                border:
                  "1px solid color-mix(in srgb, currentColor 12%, transparent)",
                display: "grid",
                gap: 4,
              }}
            >
              <AppTypography variant="caption" color="text.secondary">
                Selected volume
              </AppTypography>
              <AppTypography variant="body2" fontWeight={700}>
                {lv.name}
              </AppTypography>
              <AppTypography variant="body2" color="text.secondary">
                {lv.vgName} · {formatFileSize(lv.size)}
              </AppTypography>
              <AppTypography variant="caption" color="text.secondary">
                {lv.path}
              </AppTypography>
            </div>
          )}
          <AppTextField
            label="New Size"
            value={newSize}
            onChange={(e) => setNewSize(e.target.value)}
            placeholder="e.g., 20G, 1T"
            helperText="Use K, M, G, T suffix for size units"
            fullWidth
            size="small"
          />
          {validationError && (
            <AppAlert severity="error">{validationError}</AppAlert>
          )}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={handleClose} disabled={isResizing}>
          Cancel
        </AppButton>
        <AppButton
          onClick={handleResize}
          variant="contained"
          disabled={isResizing}
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
    deleteLV([lv.vgName, lv.name]);
  };
  const handleClose = () => {
    onClose();
  };
  return (
    <GeneralDialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
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
        <AppButton onClick={handleClose} disabled={isDeleting}>
          Cancel
        </AppButton>
        <AppButton
          onClick={handleDelete}
          variant="contained"
          color="error"
          disabled={isDeleting || !!lv?.mountpoint}
        >
          {isDeleting ? "Deleting..." : "Delete"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};
const PVTable: React.FC<{
  data: PhysicalVolume[];
}> = ({ data }) => (
  <AppTableContainer>
    <AppTable>
      <AppTableHead>
        <AppTableRow>
          <AppTableCell>Name</AppTableCell>
          <AppTableCell>Volume Group</AppTableCell>
          <AppTableCell>Size</AppTableCell>
          <AppTableCell>Free</AppTableCell>
          <AppTableCell>Format</AppTableCell>
        </AppTableRow>
      </AppTableHead>
      <AppTableBody>
        {data.length === 0 ? (
          <AppTableRow>
            <AppTableCell colSpan={5}>
              <AppTypography color="text.secondary" align="center">
                No physical volumes found
              </AppTypography>
            </AppTableCell>
          </AppTableRow>
        ) : (
          data.map((pv) => (
            <AppTableRow key={pv.name}>
              <AppTableCell>
                <AppTypography variant="body2" style={monospaceStyle}>
                  {pv.name}
                </AppTypography>
              </AppTableCell>
              <AppTableCell>{pv.vgName || "-"}</AppTableCell>
              <AppTableCell>{formatFileSize(pv.size)}</AppTableCell>
              <AppTableCell>{formatFileSize(pv.free)}</AppTableCell>
              <AppTableCell>
                <Chip label={pv.format} size="small" variant="soft" />
              </AppTableCell>
            </AppTableRow>
          ))
        )}
      </AppTableBody>
    </AppTable>
  </AppTableContainer>
);
const VGTable: React.FC<{
  data: VolumeGroup[];
}> = ({ data }) => (
  <AppTableContainer>
    <AppTable>
      <AppTableHead>
        <AppTableRow>
          <AppTableCell>Name</AppTableCell>
          <AppTableCell>Size</AppTableCell>
          <AppTableCell>Free</AppTableCell>
          <AppTableCell>PVs</AppTableCell>
          <AppTableCell>LVs</AppTableCell>
        </AppTableRow>
      </AppTableHead>
      <AppTableBody>
        {data.length === 0 ? (
          <AppTableRow>
            <AppTableCell colSpan={5}>
              <AppTypography color="text.secondary" align="center">
                No volume groups found
              </AppTypography>
            </AppTableCell>
          </AppTableRow>
        ) : (
          data.map((vg) => (
            <AppTableRow key={vg.name}>
              <AppTableCell>
                <AppTypography variant="body2" fontWeight={600}>
                  {vg.name}
                </AppTypography>
              </AppTableCell>
              <AppTableCell>{formatFileSize(vg.size)}</AppTableCell>
              <AppTableCell>{formatFileSize(vg.free)}</AppTableCell>
              <AppTableCell>{vg.pvCount}</AppTableCell>
              <AppTableCell>{vg.lvCount}</AppTableCell>
            </AppTableRow>
          ))
        )}
      </AppTableBody>
    </AppTable>
  </AppTableContainer>
);
interface LVTableProps {
  data: LogicalVolume[];
  onResize: (lv: LogicalVolume) => void;
  onDelete: (lv: LogicalVolume) => void;
}
const LVTable: React.FC<LVTableProps> = ({ data, onResize, onDelete }) => (
  <AppTableContainer>
    <AppTable>
      <AppTableHead>
        <AppTableRow>
          <AppTableCell>Name</AppTableCell>
          <AppTableCell>Volume Group</AppTableCell>
          <AppTableCell>Size</AppTableCell>
          <AppTableCell>Mountpoint</AppTableCell>
          <AppTableCell>Usage</AppTableCell>
          <AppTableCell align="right">Actions</AppTableCell>
        </AppTableRow>
      </AppTableHead>
      <AppTableBody>
        {data.length === 0 ? (
          <AppTableRow>
            <AppTableCell colSpan={6}>
              <AppTypography color="text.secondary" align="center">
                No logical volumes found
              </AppTypography>
            </AppTableCell>
          </AppTableRow>
        ) : (
          data.map((lv) => (
            <AppTableRow key={lv.path}>
              <AppTableCell>
                <AppTypography variant="body2" fontWeight={600}>
                  {lv.name}
                </AppTypography>
                <AppTypography
                  variant="caption"
                  color="text.secondary"
                  style={monospaceStyle}
                >
                  {lv.path}
                </AppTypography>
              </AppTableCell>
              <AppTableCell>{lv.vgName}</AppTableCell>
              <AppTableCell>{formatFileSize(lv.size)}</AppTableCell>
              <AppTableCell>
                {lv.mountpoint ? (
                  <AppTypography variant="body2" style={monospaceStyle}>
                    {lv.mountpoint}
                  </AppTypography>
                ) : (
                  <Chip label="Not mounted" size="small" variant="soft" />
                )}
              </AppTableCell>
              <AppTableCell>
                {lv.mountpoint ? (
                  <div
                    style={{
                      width: 100,
                    }}
                  >
                    <AppLinearProgress
                      variant="determinate"
                      value={lv.usedPct}
                      style={{
                        height: 6,
                        borderRadius: 3,
                        marginBottom: 2,
                      }}
                      color={getUsageColor(lv.usedPct)}
                    />
                    <AppTypography variant="caption">
                      {lv.usedPct.toFixed(1)}%
                    </AppTypography>
                  </div>
                ) : (
                  "-"
                )}
              </AppTableCell>
              <AppTableCell align="right">
                <div
                  style={{
                    display: "flex",
                    justifyContent: "flex-end",
                    gap: 8,
                    flexWrap: "wrap",
                  }}
                >
                  <AppButton
                    size="small"
                    variant="outlined"
                    onClick={() => onResize(lv)}
                    startIcon={
                      <Icon icon="mdi:pencil" width={18} height={18} />
                    }
                  >
                    Resize
                  </AppButton>
                  <AppButton
                    size="small"
                    variant="outlined"
                    color="error"
                    onClick={() => onDelete(lv)}
                    startIcon={
                      <Icon icon="mdi:delete" width={18} height={18} />
                    }
                  >
                    Delete
                  </AppButton>
                </div>
              </AppTableCell>
            </AppTableRow>
          ))
        )}
      </AppTableBody>
    </AppTable>
  </AppTableContainer>
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
    return <ComponentLoader />;
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
          gap: 12,
        }}
      >
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))",
            gap: 12,
          }}
        >
          <LVMMetricCard
            title="Logical Volumes"
            value={String(lvsList.length)}
            subtitle={`Provisioned ${formatFileSize(totalLvCapacity)}`}
            icon="mdi:database-outline"
            color={PANEL_ACCENTS.lvs}
          />
          <LVMMetricCard
            title="Volume Groups"
            value={String(vgsList.length)}
            subtitle={`${formatFileSize(totalVgFree)} free capacity`}
            icon="mdi:layers-triple-outline"
            color={PANEL_ACCENTS.vgs}
          />
          <LVMMetricCard
            title="Physical Volumes"
            value={String(pvsList.length)}
            subtitle={`${formatFileSize(totalPvCapacity)} raw capacity`}
            icon="mdi:harddisk"
            color={PANEL_ACCENTS.pvs}
          />
        </div>

        <LVMSectionCard
          title="Logical Volumes"
          subtitle={`${mountedLvCount} mounted across ${vgsList.length} volume group${vgsList.length === 1 ? "" : "s"}`}
          count={lvsList.length}
          icon="mdi:database-outline"
          accent={PANEL_ACCENTS.lvs}
          expanded={expanded === "lvs"}
          onToggle={() => handleSectionToggle("lvs")}
        >
          <LVTable
            data={lvsList}
            onResize={handleResize}
            onDelete={handleDelete}
          />
        </LVMSectionCard>

        <LVMSectionCard
          title="Volume Groups"
          subtitle={`${formatFileSize(totalVgFree)} free across all groups`}
          count={vgsList.length}
          icon="mdi:layers-triple-outline"
          accent={PANEL_ACCENTS.vgs}
          expanded={expanded === "vgs"}
          onToggle={() => handleSectionToggle("vgs")}
        >
          <VGTable data={vgsList} />
        </LVMSectionCard>

        <LVMSectionCard
          title="Physical Volumes"
          subtitle={`${formatFileSize(totalPvCapacity)} discovered device capacity`}
          count={pvsList.length}
          icon="mdi:harddisk"
          accent={PANEL_ACCENTS.pvs}
          expanded={expanded === "pvs"}
          onToggle={() => handleSectionToggle("pvs")}
        >
          <PVTable data={pvsList} />
        </LVMSectionCard>
      </div>

      <CreateLVDialog
        open={createDialogOpen}
        onClose={() => setCreateDialogOpen(false)}
        volumeGroups={vgsList}
        onSuccess={handleRefreshAll}
      />

      <ResizeLVDialog
        open={resizeDialogOpen}
        onClose={() => setResizeDialogOpen(false)}
        lv={selectedLV}
        onSuccess={handleRefreshAll}
      />

      <DeleteLVDialog
        open={deleteDialogOpen}
        onClose={() => setDeleteDialogOpen(false)}
        lv={selectedLV}
        onSuccess={handleRefreshAll}
      />
    </>
  );
};
export default LVMManagement;
