import { type DockerNetwork } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import SelectableCard from "@/components/cards/SelectableCard";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import { CARD_PADDING_SM } from "@/theme/constants";
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

const DOCKER_TOAST_META = { label: "Open Docker", to: "/docker" } as const;

const NetworkCard = ({ network, selected, onSelect }: NetworkCardProps) => (
  <SelectableCard
    label={`network ${network.Name}`}
    onSelect={onSelect}
    selected={selected}
  >
    <FrostedCard
      accent
      hoverLift
      style={{
        padding: CARD_PADDING_SM,
        /*
          Selection takes the accent line to full strength and holds the lift
          shadow, the same treatment DockerImageCard gives its selected state.
        */
        ...(selected && {
          borderBottomColor: "var(--fc-accent)",
          boxShadow: "var(--fc-lift-shadow)",
        }),
      }}
    >
      {/* Header: name + driver chip */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          gap: 8,
          marginBottom: 8,
        }}
      >
        <AppTypography
          fontWeight={700}
          noWrap
          title={network.Name}
          toastMeta={DOCKER_TOAST_META}
          variant="body2"
        >
          {network.Name}
        </AppTypography>
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

      {/* ID */}
      <AppTypography
        style={{
          marginTop: 4,
          marginBottom: 4,
          fontFamily: "var(--app-font-mono)",
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
  </SelectableCard>
);

export default NetworkCard;
