import { type DockerNetwork } from "@/api";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import {
  LONG_TEXT_CLASS,
  RESPONSIVE_TEXT_CLASS,
  wrappableChipLabelStyle,
  wrappableChipStyle,
} from "@/components/tables/wrappingText";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";

import "./network-list.css";

export interface ConnectedContainerRow {
  endpointId: string;
  id: string;
  ipv4: string;
  ipv6: string;
  mac: string;
  name: string;
}

const connectedContainerColumns: AppVirtualTableColumnDef<ConnectedContainerRow>[] =
  [
    {
      accessorKey: "name",
      header: "Name",
      cell: ({ row }) => (
        <AppTypography className={RESPONSIVE_TEXT_CLASS} variant="body2">
          {row.original.name}
        </AppTypography>
      ),
    },
    {
      accessorKey: "id",
      header: "Container ID",
      cell: ({ row }) => (
        <span
          className={LONG_TEXT_CLASS}
          style={{ fontFamily: "var(--app-font-mono)" }}
        >
          {row.original.id.slice(0, 12)}
        </span>
      ),
    },
    {
      accessorKey: "endpointId",
      header: "Endpoint ID",
      cell: ({ row }) => (
        <span
          className={LONG_TEXT_CLASS}
          style={{ fontFamily: "var(--app-font-mono)" }}
        >
          {row.original.endpointId.slice(0, 12) || "-"}
        </span>
      ),
    },
    {
      accessorKey: "ipv4",
      header: "IPv4",
      cell: ({ row }) => (
        <AppTypography
          className={LONG_TEXT_CLASS}
          style={{ fontFamily: "var(--app-font-mono)" }}
          variant="body2"
        >
          {row.original.ipv4}
        </AppTypography>
      ),
    },
    {
      accessorKey: "ipv6",
      header: "IPv6",
      cell: ({ row }) => (
        <AppTypography
          className={LONG_TEXT_CLASS}
          style={{ fontFamily: "var(--app-font-mono)" }}
          variant="body2"
        >
          {row.original.ipv6}
        </AppTypography>
      ),
    },
    {
      accessorKey: "mac",
      header: "MAC",
      cell: ({ row }) => (
        <span
          className={LONG_TEXT_CLASS}
          style={{ fontFamily: "var(--app-font-mono)" }}
        >
          {row.original.mac}
        </span>
      ),
    },
  ];

export const NetworkDetailsContent = ({
  connectDisabled,
  network,
  onConnect,
  onDisconnect,
}: {
  connectDisabled: boolean;
  network: DockerNetwork;
  onConnect: () => void;
  onDisconnect: (container: ConnectedContainerRow) => void;
}) => {
  const columns: AppVirtualTableColumnDef<ConnectedContainerRow>[] = [
    ...connectedContainerColumns,
    {
      id: "actions",
      header: "Actions",
      enableSorting: false,
      cell: ({ row }) => (
        <AppActionIconButton
          ariaLabel={`Disconnect container ${row.original.name}`}
          color="var(--app-palette-error-main)"
          icon="mdi:lan-disconnect"
          iconSize={18}
          label="Disconnect container"
          onClick={() => onDisconnect(row.original)}
        />
      ),
      meta: { align: "right", width: "80px" },
    },
  ];
  return (
    <div className="expand-panel">
      <div>
        <AppTypography gutterBottom variant="subtitle2">
          <b>Full Network ID:</b>
        </AppTypography>
        <AppTypography
          className={`expand-panel__mono ${LONG_TEXT_CLASS}`}
          variant="body2"
        >
          {network.Id}
        </AppTypography>
      </div>
      <div>
        <AppTypography gutterBottom variant="subtitle2">
          <b>Subnet(s):</b>
        </AppTypography>
        <div className="expand-panel__chips">
          {network.IPAM?.Config?.length ? (
            network.IPAM.Config.map((ipam, index) => (
              <Chip
                key={index}
                label={`${ipam.Subnet} / Gateway: ${ipam.Gateway}`}
                size="small"
                style={wrappableChipStyle}
                labelStyle={wrappableChipLabelStyle}
                variant="soft"
              />
            ))
          ) : (
            <AppTypography color="text.secondary" variant="body2">
              (no IPAM config)
            </AppTypography>
          )}
        </div>
      </div>
      <div>
        <AppTypography gutterBottom variant="subtitle2">
          <b>Network Details:</b>
        </AppTypography>
        <div className="expand-panel__chips">
          {network.Created && (
            <Chip
              label={`Created: ${new Date(network.Created).toLocaleString()}`}
              size="small"
              variant="soft"
            />
          )}
          <Chip
            label={`Driver: ${network.Driver}`}
            size="small"
            variant="soft"
          />
          <Chip label={`Scope: ${network.Scope}`} size="small" variant="soft" />
          <Chip
            label={`Internal: ${network.Internal ? "Yes" : "No"}`}
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
        {network.Protected && (
          <AppTypography
            color="text.secondary"
            style={{ marginTop: "var(--app-space-4)" }}
            variant="body2"
          >
            Docker default networks cannot be deleted.
          </AppTypography>
        )}
      </div>
      <div>
        <AppTypography gutterBottom variant="subtitle2">
          <b>Options:</b>
        </AppTypography>
        <div className="expand-panel__chips">
          {Object.entries(network.Options ?? {}).length ? (
            Object.entries(network.Options ?? {}).map(([key, value]) => (
              <Chip
                key={key}
                label={`${key}: ${value}`}
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
      {(network.IPAM?.Driver ||
        Object.keys(network.IPAM?.Options ?? {}).length > 0) && (
        <div>
          <AppTypography gutterBottom variant="subtitle2">
            <b>IPAM:</b>
          </AppTypography>
          <div className="expand-panel__chips">
            {network.IPAM?.Driver && (
              <Chip
                label={`Driver: ${network.IPAM.Driver}`}
                size="small"
                variant="soft"
              />
            )}
            {Object.entries(network.IPAM?.Options ?? {}).map(([key, value]) => (
              <Chip
                key={key}
                label={`${key}: ${value}`}
                size="small"
                style={wrappableChipStyle}
                labelStyle={wrappableChipLabelStyle}
                variant="soft"
              />
            ))}
          </div>
        </div>
      )}
      {network.IPAM?.Config?.some(
        (config) =>
          config.IPRange ||
          Object.keys(config.AuxiliaryAddresses ?? {}).length > 0,
      ) && (
        <div>
          <AppTypography gutterBottom variant="subtitle2">
            <b>IPAM Ranges:</b>
          </AppTypography>
          <div className="expand-panel__chips">
            {network.IPAM.Config.flatMap((config, index) => [
              ...(config.IPRange
                ? [
                    <Chip
                      key={`${index}-range`}
                      label={`Range: ${config.IPRange}`}
                      size="small"
                      variant="soft"
                    />,
                  ]
                : []),
              ...Object.entries(config.AuxiliaryAddresses ?? {}).map(
                ([key, value]) => (
                  <Chip
                    key={`${index}-${key}`}
                    label={`${key}: ${value}`}
                    size="small"
                    style={wrappableChipStyle}
                    labelStyle={wrappableChipLabelStyle}
                    variant="soft"
                  />
                ),
              ),
            ])}
          </div>
        </div>
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
          {Object.entries(network.Labels ?? {}).length ? (
            Object.entries(network.Labels ?? {}).map(([key, value]) => (
              <Chip
                key={key}
                label={`${key}: ${value}`}
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
        <div
          style={{
            alignItems: "center",
            display: "flex",
            gap: "var(--app-space-8)",
            justifyContent: "space-between",
          }}
        >
          <AppTypography gutterBottom variant="subtitle2">
            <b>Connected Containers:</b>
          </AppTypography>
          <AppButton
            disabled={connectDisabled}
            onClick={onConnect}
            size="small"
            variant="outlined"
          >
            Connect container
          </AppButton>
        </div>
        {Object.entries(network.Containers ?? {}).length ? (
          <AppVirtualTable
            ariaLabel="Connected containers"
            className="network-connected-table"
            columns={columns}
            data={Object.entries(network.Containers ?? {}).map(
              ([id, info]) => ({
                endpointId: info.EndpointID || "",
                id,
                ipv4: info.IPv4Address?.replace(/\/.*/, "") || "-",
                ipv6: info.IPv6Address?.replace(/\/.*/, "") || "-",
                mac: info.MacAddress || "-",
                name: info.Name || "-",
              }),
            )}
            density="compact"
            fillAvailable={false}
            getRowId={(container) => container.id}
            maxHeight={240}
            variant="embedded"
          />
        ) : (
          <AppTypography color="text.secondary" variant="body2">
            (no containers)
          </AppTypography>
        )}
      </div>
    </div>
  );
};
