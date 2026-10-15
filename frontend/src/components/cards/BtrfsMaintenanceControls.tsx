import { Icon } from "@iconify/react";
import { Link } from "@tanstack/react-router";
import { Fragment } from "react";

import type { BtrfsMaintenanceSchedule } from "@/api";
import AppAlert from "@/components/ui/AppAlert";
import AppDivider from "@/components/ui/AppDivider";
import AppSelect from "@/components/ui/AppSelect";
import AppTypography from "@/components/ui/AppTypography";

interface TaskInfo {
  label: string;
  icon: string;
  description: string;
  /** Extra caution shown in the warning colour. */
  caution?: string;
  /** Upstream btrfsmaintenance default for the task. */
  suggested: string;
}

const TASK_INFO: Record<string, TaskInfo> = {
  scrub: {
    label: "Scrub",
    icon: "mdi:shield-check-outline",
    description:
      "Reads every block and checks it against its checksum to catch silent corruption. On RAID1 or DUP, bad copies are repaired from the good one.",
    suggested: "monthly",
  },
  balance: {
    label: "Balance",
    icon: "mdi:scale-balance",
    description:
      "Btrfs hands out space in chunks. Balance repacks nearly empty chunks so their space can be reused, which prevents “no space left” errors while free space still shows. Only light filters are used, so runs stay short.",
    suggested: "weekly",
  },
  trim: {
    label: "Trim",
    icon: "mdi:content-cut",
    description:
      "Tells an SSD which blocks are free so it keeps write speed up. Hard drives don't need it, and on kernel 6.2 or later btrfs already discards freed space on SSDs by itself.",
    suggested: "none",
  },
  defrag: {
    label: "Defrag",
    icon: "mdi:puzzle-outline",
    description: "Rewrites fragmented files to speed up reads on hard drives.",
    caution:
      "Breaks sharing with snapshots and reflinked copies, which can use a lot of extra space.",
    suggested: "none",
  },
};

const periodLabel = (period: string) =>
  period === "none" ? "Off" : period.charAt(0).toUpperCase() + period.slice(1);

export interface BtrfsMaintenanceProps {
  /** Schedule from the backend, or null while loading / unavailable. */
  btrfsMaintenance: BtrfsMaintenanceSchedule | null;
  /** Capability reason when btrfsmaintenance is not usable; null when it is. */
  btrfsMaintenanceReason: string | null;
  isUpdatingMaintenance: boolean;
  onSetMaintenance: (mountpoint: string, task: string, period: string) => void;
}

const BtrfsMaintenanceControls = ({
  mountpoint,
  btrfsMaintenance,
  btrfsMaintenanceReason,
  isUpdatingMaintenance,
  onSetMaintenance,
}: BtrfsMaintenanceProps & { mountpoint: string }) => {
  if (!btrfsMaintenanceReason && !btrfsMaintenance) return null;
  return (
    <section
      aria-label="Scheduled maintenance"
      style={{ marginTop: "var(--app-space-24)" }}
    >
      <AppTypography fontWeight={600} variant="subtitle2">
        Scheduled maintenance
      </AppTypography>
      <AppTypography
        color="text.secondary"
        style={{ display: "block", marginBottom: "var(--app-space-12)" }}
        variant="caption"
      >
        Background jobs from btrfsmaintenance, run by systemd timers.
      </AppTypography>
      {btrfsMaintenanceReason ? (
        <AppAlert severity="info">
          {btrfsMaintenanceReason}{" "}
          <Link search={{ tab: "capabilities" }} to="/settings">
            Open Capabilities to install btrfsmaintenance
          </Link>
        </AppAlert>
      ) : (
        <div
          style={{
            border: "1px solid var(--app-palette-divider)",
            borderRadius: "var(--app-radius-md)",
          }}
        >
          {btrfsMaintenance?.tasks.map((task, index) => {
            const info = TASK_INFO[task.task];
            const value = task.mountpoints.includes(mountpoint)
              ? task.period
              : "none";
            const sharedWith = task.mountpoints.filter((m) => m !== mountpoint);
            return (
              <Fragment key={task.task}>
                {index > 0 && <AppDivider />}
                <div
                  style={{
                    display: "flex",
                    flexWrap: "wrap",
                    gap: "var(--app-space-8) var(--app-space-16)",
                    padding: "var(--app-space-12) var(--app-space-16)",
                  }}
                >
                  <div
                    style={{
                      display: "flex",
                      flex: "1 1 260px",
                      gap: "var(--app-space-12)",
                      minWidth: 0,
                    }}
                  >
                    <Icon
                      aria-hidden="true"
                      height={20}
                      icon={info.icon}
                      style={{
                        color:
                          value === "none"
                            ? "var(--app-palette-text-secondary)"
                            : "var(--app-palette-primary-main)",
                        flexShrink: 0,
                        marginTop: 2,
                      }}
                      width={20}
                    />
                    <div style={{ minWidth: 0 }}>
                      <AppTypography fontWeight={600} variant="body2">
                        {info.label}
                      </AppTypography>
                      <AppTypography
                        color="text.secondary"
                        style={{ display: "block" }}
                        variant="caption"
                      >
                        {info.description}
                      </AppTypography>
                      {info.caution && (
                        <AppTypography
                          color="warning"
                          style={{ display: "block" }}
                          variant="caption"
                        >
                          {info.caution}
                        </AppTypography>
                      )}
                      <AppTypography
                        color="text.secondary"
                        style={{
                          display: "block",
                          marginTop: "var(--app-space-4)",
                        }}
                        variant="caption"
                      >
                        Suggested: {periodLabel(info.suggested)}
                        {sharedWith.length > 0 &&
                          ` · Shared with ${sharedWith.join(", ")}, so a new period applies there too`}
                      </AppTypography>
                    </div>
                  </div>
                  <div
                    style={{
                      alignSelf: "center",
                      flex: "0 0 140px",
                      marginInlineStart: "auto",
                    }}
                  >
                    <AppSelect
                      aria-label={`${info.label} period`}
                      disabled={isUpdatingMaintenance}
                      fullWidth
                      onChange={(event) =>
                        onSetMaintenance(
                          mountpoint,
                          task.task,
                          event.target.value,
                        )
                      }
                      size="small"
                      value={value}
                    >
                      {task.periods.map((period) => (
                        <option key={period} value={period}>
                          {periodLabel(period)}
                        </option>
                      ))}
                    </AppSelect>
                  </div>
                </div>
              </Fragment>
            );
          })}
        </div>
      )}
    </section>
  );
};

export default BtrfsMaintenanceControls;
