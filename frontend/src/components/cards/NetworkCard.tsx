import type { ReactNode } from "react";

import { type DockerNetwork } from "@/api";
import DockerResourceCard from "@/components/cards/DockerResourceCard";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import { GAP_SM } from "@/theme/constants";
import {
  longTextStyles,
  wrappableChipStyle,
  wrappableChipLabelStyle,
} from "@/theme/tableStyles";

export interface NetworkCardProps {
  actions?: ReactNode;
  network: DockerNetwork;
  onOpen?: () => void;
  selected?: boolean;
}

const NetworkCard = ({ actions, network, selected, onOpen }: NetworkCardProps) => (
  <DockerResourceCard
    icon="mdi:lan"
    actions={actions}
    label={`network ${network.Name}`}
    onOpen={onOpen}
    selected={selected}
    subtitle={`${network.Driver} · ${network.Scope}`}
    title={network.Name}
  >
    <div style={{ display: "flex", flexWrap: "wrap", gap: GAP_SM }}>
      <Chip
        label={`Internal: ${network.Internal ? "Yes" : "No"}`}
        size="small"
        variant="soft"
      />
      <Chip
        label={`IPv4: ${network.EnableIPv4 !== false ? "Yes" : "No"}`}
        size="small"
        variant="soft"
      />
      <Chip
        label={`IPv6: ${network.EnableIPv6 ? "Yes" : "No"}`}
        size="small"
        variant="soft"
      />
      <Chip
        label={`Attachable: ${network.Attachable ? "Yes" : "No"}`}
        size="small"
        variant="soft"
      />
      <Chip
        label={`Ingress: ${network.Ingress ? "Yes" : "No"}`}
        size="small"
        variant="soft"
      />
      <Chip
        label={`Config only: ${network.ConfigOnly ? "Yes" : "No"}`}
        size="small"
        variant="soft"
      />
      {network.Created && (
        <Chip
          label={`Created: ${new Date(network.Created).toLocaleDateString()}`}
          size="small"
          variant="soft"
        />
      )}
    </div>

    <AppTypography
      color="text.secondary"
      style={{
        marginTop: GAP_SM,
        marginBottom: GAP_SM,
        fontFamily: "var(--app-font-mono)",
        ...longTextStyles,
      }}
      variant="body2"
    >
      ID: {network.Id}
    </AppTypography>

    <div style={{ display: "flex", flexWrap: "wrap", gap: GAP_SM }}>
      {network.IPAM?.Config && network.IPAM.Config.length > 0 ? (
        network.IPAM.Config.slice(0, 2).map((ipam, i) => (
          <Chip
            key={`${network.Id}-ipam-${i}`}
            label={ipam.Subnet}
            size="small"
            style={wrappableChipStyle}
            labelStyle={wrappableChipLabelStyle}
            variant="outlined"
          />
        ))
      ) : (
        <AppTypography color="text.secondary" variant="caption">
          No IPAM config
        </AppTypography>
      )}
    </div>
  </DockerResourceCard>
);

export default NetworkCard;
