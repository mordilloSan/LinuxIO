import { type DockerVolume } from "@/api";
import {
  LONG_TEXT_CLASS,
  wrappableChipLabelStyle,
  wrappableChipStyle,
} from "@/components/tables/wrappingText";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import { DetailRow } from "@/components/ui/InfoRow";

import {
  formatDockerValue,
  formatReferenceCount,
  formatVolumeSize,
} from "./volumeFormat";

export const VolumeDetailsContent = ({ volume }: { volume: DockerVolume }) => (
  <div className="expand-panel">
    <div>
      <AppTypography gutterBottom variant="subtitle2">
        <b>Full Mountpoint:</b>
      </AppTypography>
      <AppTypography
        className={`expand-panel__mono ${LONG_TEXT_CLASS}`}
        variant="body2"
      >
        {volume.Mountpoint || "-"}
      </AppTypography>
    </div>
    {volume.CreatedAt && (
      <div>
        <AppTypography gutterBottom variant="subtitle2">
          <b>Created:</b>
        </AppTypography>
        <AppTypography className="expand-panel__mono" variant="body2">
          {new Date(volume.CreatedAt).toLocaleString()}
        </AppTypography>
      </div>
    )}
    <div>
      <AppTypography gutterBottom variant="subtitle2">
        <b>Usage:</b>
      </AppTypography>
      <div className="expand-panel__chips">
        <Chip
          label={`Size: ${formatVolumeSize(volume.UsageData?.Size)}`}
          size="small"
          variant="soft"
        />
        <Chip
          label={`References: ${formatReferenceCount(volume.UsageData?.RefCount)}`}
          size="small"
          variant="soft"
        />
      </div>
    </div>
    <div>
      <AppTypography gutterBottom variant="subtitle2">
        <b>Containers:</b>
      </AppTypography>
      {volume.Containers?.length ? (
        <div>
          {volume.Containers.map((container, index) => (
            <DetailRow
              key={container.Id}
              label={container.Name}
              noBorder={index === 0}
              split
            >
              <Chip
                color={container.State === "running" ? "success" : "default"}
                label={container.State || "unknown"}
                size="xsmall"
                variant="soft"
              />
            </DetailRow>
          ))}
        </div>
      ) : (
        <AppTypography color="text.secondary" variant="body2">
          No containers use this volume.
        </AppTypography>
      )}
    </div>
    {!volume.MountpointAccessible && (
      <AppTypography color="text.secondary" variant="body2">
        This volume does not expose an accessible host mountpoint, so browsing
        and backups are unavailable.
      </AppTypography>
    )}
    <div>
      <AppTypography gutterBottom variant="subtitle2">
        <b>Labels:</b>
      </AppTypography>
      <div
        style={{
          alignItems: "flex-start",
          display: "flex",
          flexDirection: "column",
          gap: "var(--app-space-4)",
        }}
      >
        {volume.Labels && Object.keys(volume.Labels).length > 0 ? (
          Object.entries(volume.Labels).map(([key, val]) => (
            <Chip
              key={key}
              label={`${key}: ${val}`}
              size="small"
              style={wrappableChipStyle}
              labelStyle={wrappableChipLabelStyle}
              variant="soft"
            />
          ))
        ) : (
          <AppTypography color="text.secondary" variant="body2">
            (no labels)
          </AppTypography>
        )}
      </div>
    </div>
    <div>
      <AppTypography gutterBottom variant="subtitle2">
        <b>Options:</b>
      </AppTypography>
      <div className="expand-panel__chips">
        {volume.Options && Object.keys(volume.Options).length > 0 ? (
          Object.entries(volume.Options).map(([key, val]) => (
            <Chip
              key={key}
              label={`${key}: ${val}`}
              size="small"
              style={wrappableChipStyle}
              labelStyle={wrappableChipLabelStyle}
              variant="soft"
            />
          ))
        ) : (
          <AppTypography color="text.secondary" variant="body2">
            (no options)
          </AppTypography>
        )}
      </div>
    </div>
    <div>
      <AppTypography gutterBottom variant="subtitle2">
        <b>Driver Status:</b>
      </AppTypography>
      <div className="expand-panel__chips">
        {volume.Status && Object.keys(volume.Status).length > 0 ? (
          Object.entries(volume.Status).map(([key, value]) => (
            <Chip
              key={key}
              label={`${key}: ${formatDockerValue(value)}`}
              size="small"
              style={wrappableChipStyle}
              labelStyle={wrappableChipLabelStyle}
              variant="soft"
            />
          ))
        ) : (
          <AppTypography color="text.secondary" variant="body2">
            (no driver status)
          </AppTypography>
        )}
      </div>
    </div>
    {volume.ClusterVolume && (
      <div>
        <AppTypography gutterBottom variant="subtitle2">
          <b>Cluster Volume:</b>
        </AppTypography>
        <div className="expand-panel__chips">
          {Object.entries(volume.ClusterVolume).map(([key, value]) => (
            <Chip
              key={key}
              label={`${key}: ${formatDockerValue(value)}`}
              size="small"
              style={wrappableChipStyle}
              labelStyle={wrappableChipLabelStyle}
              variant="soft"
            />
          ))}
        </div>
      </div>
    )}
  </div>
);
