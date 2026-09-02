import { Fragment, useMemo, type CSSProperties, type ReactNode } from "react";

import type { ContainerInfo, ContainerPort } from "@/api";
import { DetailRow } from "@/components/cards/UnitInfoPanelCard";
import MetricBar from "@/components/gauge/MetricBar";
import Chip from "@/components/ui/AppChip";
import AppDivider from "@/components/ui/AppDivider";
import AppTypography from "@/components/ui/AppTypography";
import InfoRow from "@/components/ui/InfoRow";
import { getDedupedPorts } from "@/utils/dockerContainer";
import { formatFileSize, formatThroughput } from "@/utils/formaters";

export type ContainerInfoSection =
  | "overview"
  | "monitoring"
  | "ports"
  | "networks"
  | "volumes";

const formatUptime = (createdUnix: number) => {
  const secs = Math.floor(Date.now() / 1000) - createdUnix;
  if (secs < 0) return "-";
  if (secs < 60) return `${secs}s`;
  const minutes = Math.floor(secs / 60) % 60;
  const hours = Math.floor(secs / 3600) % 24;
  const days = Math.floor(secs / 86400);
  if (days > 0) return `${days}d ${hours}h`;
  if (hours > 0) return `${hours}h ${minutes}m`;
  return `${minutes}m`;
};

const formatPort = (port: ContainerPort) =>
  port.PublicPort
    ? `${port.PublicPort}:${port.PrivatePort}/${port.Type}`
    : `${port.PrivatePort}/${port.Type}`;

const SectionTitle = ({ children }: { children: ReactNode }) => (
  <AppTypography fontWeight={700} style={{ margin: 0 }} variant="subtitle2">
    {children}
  </AppTypography>
);

const emptyText = (text: string) => (
  <AppTypography color="text.secondary" variant="body2">
    {text}
  </AppTypography>
);

/** Single-line value that truncates and exposes the full text via a copy tooltip. */
const TruncatedValue = ({ text }: { text: string }) => (
  <AppTypography
    component="div"
    fontWeight={500}
    noWrap
    title={text}
    variant="caption"
  >
    {text}
  </AppTypography>
);

interface ContainerInfoSectionsProps {
  container: ContainerInfo;
  sections: ContainerInfoSection[];
}

/**
 * Renders the requested container detail sections with dividers between them.
 * Shared by the selected container card (config sections) and the monitoring
 * panel so the two stay in sync.
 */
const ContainerInfoSections = ({
  container,
  sections,
}: ContainerInfoSectionsProps) => {
  const ports = useMemo(() => getDedupedPorts(container), [container]);
  const networks = useMemo(
    () => Object.entries(container.NetworkSettings?.Networks ?? {}),
    [container.NetworkSettings],
  );
  const volumes = useMemo(
    () =>
      (container.Mounts ?? []).filter(
        (mount) => mount.Type === "bind" || mount.Type === "volume",
      ),
    [container.Mounts],
  );

  const metrics = container.metrics;
  const cpuPercent = metrics?.cpu_percent;
  const memUsage = metrics?.memory_usage_bytes;
  const memLimit = metrics?.memory_limit_bytes;
  const memPercent =
    memUsage !== undefined && memLimit !== undefined && memLimit > 0
      ? Math.min((memUsage / memLimit) * 100, 100)
      : 0;
  const memoryTooltip =
    memUsage === undefined
      ? "Memory metrics unavailable"
      : memLimit === undefined
        ? `Memory Usage: ${formatFileSize(memUsage)} (limit unavailable)`
        : `Memory Usage: ${formatFileSize(memUsage)} / ${formatFileSize(memLimit)}`;
  const formatRate = (value?: number) =>
    value === undefined ? "Unavailable" : formatThroughput(value);

  const sectionStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    gap: "var(--app-space-4)",
    minWidth: 0,
  };

  const networkLabelStyle: CSSProperties = {
    textTransform: "uppercase",
    letterSpacing: "0.06em",
    flexShrink: 1,
    minWidth: 0,
    maxWidth: "60%",
  };
  const renderSection = (section: ContainerInfoSection): ReactNode => {
    switch (section) {
      case "overview":
        return (
          <div style={sectionStyle}>
            <SectionTitle>Overview</SectionTitle>
            <div>
              <DetailRow label="ID" noBorder>
                <TruncatedValue text={container.Id} />
              </DetailRow>
              <DetailRow label="Image Tag">
                <TruncatedValue text={container.Image} />
              </DetailRow>
              <DetailRow label="Uptime">
                <AppTypography
                  component="span"
                  fontWeight={500}
                  variant="caption"
                >
                  {formatUptime(container.Created)}
                </AppTypography>
              </DetailRow>
            </div>
          </div>
        );
      case "monitoring":
        return (
          <div style={sectionStyle}>
            <SectionTitle>Monitoring</SectionTitle>
            <MetricBar
              color="var(--app-palette-primary-main)"
              label="CPU"
              percent={cpuPercent ?? 0}
              rightLabel={
                cpuPercent === undefined
                  ? "Unavailable"
                  : `${cpuPercent.toFixed(1)}%`
              }
              tooltip="CPU Usage"
            />
            <MetricBar
              color="var(--app-palette-primary-main)"
              label="MEM"
              percent={memPercent}
              rightLabel={
                memUsage === undefined
                  ? "Unavailable"
                  : formatFileSize(memUsage)
              }
              tooltip={memoryTooltip}
            />
            <AppDivider style={{ marginBlock: "var(--app-space-4)" }} />
            <InfoRow label="Net Receive rate">
              {formatRate(metrics?.network_receive_bytes_per_second)}
            </InfoRow>
            <InfoRow label="Net Send rate">
              {formatRate(metrics?.network_send_bytes_per_second)}
            </InfoRow>
            <InfoRow label="Block Read rate">
              {formatRate(metrics?.block_read_bytes_per_second)}
            </InfoRow>
            <InfoRow label="Block Write rate" noBorder>
              {formatRate(metrics?.block_write_bytes_per_second)}
            </InfoRow>
          </div>
        );
      case "ports":
        return (
          <div style={sectionStyle}>
            <SectionTitle>Ports</SectionTitle>
            {ports.length > 0 ? (
              <div
                style={{
                  display: "flex",
                  flexWrap: "wrap",
                  gap: 6,
                  minWidth: 0,
                }}
              >
                {ports.map((port) => (
                  <Chip
                    key={`${port.PrivatePort}-${port.PublicPort ?? "private"}-${port.Type}`}
                    label={formatPort(port)}
                    size="xsmall"
                    variant="soft"
                  />
                ))}
              </div>
            ) : (
              emptyText("No published ports.")
            )}
          </div>
        );
      case "networks":
        return (
          <div style={sectionStyle}>
            <SectionTitle>Networks</SectionTitle>
            {networks.length > 0 ? (
              <div>
                {networks.map(([networkName, endpoint], index) => (
                  <div
                    key={networkName}
                    className="svc-detail-row"
                    style={{
                      display: "flex",
                      padding: "3px 0",
                      borderTop:
                        index === 0
                          ? undefined
                          : "1px solid var(--app-palette-divider)",
                      alignItems: "baseline",
                      gap: "var(--app-space-8)",
                      minWidth: 0,
                    }}
                  >
                    <AppTypography
                      color="text.secondary"
                      component="span"
                      noWrap
                      style={networkLabelStyle}
                      title={networkName}
                      variant="caption"
                    >
                      {networkName}
                    </AppTypography>
                    <AppTypography
                      component="span"
                      fontWeight={500}
                      variant="caption"
                    >
                      {endpoint.IPAddress || "-"}
                    </AppTypography>
                  </div>
                ))}
              </div>
            ) : (
              emptyText("No networks attached.")
            )}
          </div>
        );
      case "volumes":
        return (
          <div style={sectionStyle}>
            <SectionTitle>Volumes</SectionTitle>
            {volumes.length > 0 ? (
              <div>
                {volumes.map((mount, index) => (
                  <DetailRow
                    key={`${mount.Source}-${mount.Destination}`}
                    label={mount.Type}
                    noBorder={index === 0}
                  >
                    <TruncatedValue
                      text={`${mount.Source} -> ${mount.Destination}`}
                    />
                  </DetailRow>
                ))}
              </div>
            ) : (
              emptyText("No volumes.")
            )}
          </div>
        );
      default:
        return null;
    }
  };

  return (
    <>
      {sections.map((section) => (
        <Fragment key={section}>{renderSection(section)}</Fragment>
      ))}
    </>
  );
};

export default ContainerInfoSections;
