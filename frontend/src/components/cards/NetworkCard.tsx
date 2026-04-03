import React from "react";

import { type DockerNetwork } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppCheckbox from "@/components/ui/AppCheckbox";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import { longTextStyles, wrappableChipStyles } from "@/theme/tableStyles";

export interface NetworkCardProps {
  network: DockerNetwork;
  selected: boolean;
  onSelect: (checked: boolean) => void;
}

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
          size="small"
          checked={selected}
          onChange={(e) => onSelect(e.target.checked)}
        />
        <AppTypography variant="body2" fontWeight={700} noWrap>
          {network.Name}
        </AppTypography>
      </div>
      <Chip
        label={network.Driver}
        size="small"
        color="primary"
        variant="soft"
        style={{ fontSize: "0.75rem" }}
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
      variant="body2"
      style={{
        marginTop: 4,
        marginBottom: 4,
        fontFamily: "monospace",
        fontSize: "0.78rem",
        ...longTextStyles,
      }}
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
            variant="outlined"
            sx={wrappableChipStyles}
          />
        ))
      ) : (
        <AppTypography variant="caption" color="text.secondary">
          No IPAM config
        </AppTypography>
      )}
    </div>
  </FrostedCard>
);

export default NetworkCard;
