import React from "react";

import { type DockerNetwork } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppCheckbox from "@/components/ui/AppCheckbox";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import {
  longTextStyles,
  wrappableChipStyle,
  wrappableChipLabelStyle,
} from "@/theme/tableStyles";

export interface NetworkCardProps {
  network: DockerNetwork;
  onSelect: (checked: boolean) => void;
  selected: boolean;
}

const DOCKER_TOAST_META = { href: "/docker", label: "Open Docker" };

const NetworkCard: React.FC<NetworkCardProps> = ({
  network,
  selected,
  onSelect,
}) => (
  <FrostedCard style={{ padding: 8 }}>
    {/* Header: checkbox + name + driver chip */}
    <div
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        gap: 8,
        marginBottom: 8,
      }}
    >
      <div
        style={{ display: "flex", alignItems: "center", gap: 8, minWidth: 0 }}
      >
        <AppCheckbox
          checked={selected}
          onChange={(e) => onSelect(e.target.checked)}
          size="small"
        />
        <AppTypography
          copyText={network.Name}
          fontWeight={700}
          noWrap
          title={network.Name}
          toastMeta={DOCKER_TOAST_META}
          variant="body2"
        >
          {network.Name}
        </AppTypography>
      </div>
      <Chip
        color="primary"
        label={network.Driver}
        size="small"
        style={{ fontSize: "0.75rem" }}
        variant="soft"
      />
    </div>

    {/* Network flags */}
    <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
      <Chip label={`Scope: ${network.Scope}`} size="small" variant="soft" />
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
    </div>

    {/* ID */}
    <AppTypography
      style={{
        marginTop: 4,
        marginBottom: 4,
        fontFamily: "monospace",
        fontSize: "0.78rem",
        ...longTextStyles,
      }}
      variant="body2"
    >
      ID: {network.Id}
    </AppTypography>

    {/* IPAM subnets */}
    <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
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
  </FrostedCard>
);

export default NetworkCard;
