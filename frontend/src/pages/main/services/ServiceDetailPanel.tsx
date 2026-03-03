import CloseIcon from "@mui/icons-material/Close";
import React from "react";

import { linuxio } from "@/api";
import type { Service } from "@/api";
import FrostedCard from "@/components/cards/RootCard";

interface ServiceDetailPanelProps {
  service: Service;
  onClose: () => void;
}

const labelStyle: React.CSSProperties = {
  textTransform: "uppercase",
  letterSpacing: "0.06em",
  fontSize: "0.6rem",
  color: "var(--mui-palette-text-secondary)",
  flexShrink: 0,
  width: 90,
  paddingTop: 3,
};

const Row: React.FC<{
  label: string;
  children: React.ReactNode;
  noBorder?: boolean;
}> = ({ label, children, noBorder }) => (
  <div
    style={{
      display: "flex",
      padding: "1px 0",
      borderTop: noBorder ? undefined : "1px solid var(--mui-palette-divider)",
      alignItems: "flex-start",
    }}
  >
    <span style={labelStyle}>{label}</span>
    <div style={{ flex: 1, minWidth: 0 }}>{children}</div>
  </div>
);

const toStringArray = (val: unknown): string[] => {
  if (!Array.isArray(val)) return [];
  return val.filter((v): v is string => typeof v === "string" && v.length > 0);
};

const DEP_FIELDS: Array<{ label: string; key: string }> = [
  { label: "Requires", key: "Requires" },
  { label: "Wants", key: "Wants" },
  { label: "Wanted by", key: "WantedBy" },
  { label: "Triggered by", key: "TriggeredBy" },
  { label: "Part of", key: "PartOf" },
  { label: "Conflicts", key: "Conflicts" },
  { label: "Before", key: "Before" },
  { label: "After", key: "After" },
];

const ServiceDetailPanel: React.FC<ServiceDetailPanelProps> = ({
  service,
  onClose,
}) => {
  const { data: info, isPending } = linuxio.dbus.get_service_info.useQuery(
    service.name,
    { refetchInterval: 2000 },
  );

  const fragmentPath = String(info?.FragmentPath ?? "");
  return (
    <FrostedCard sx={{ p: 3, height: "100%", display: "flex", flexDirection: "column" }}>
      {/* Header */}
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "flex-start",
          marginBottom: 12,
          gap: 8,
        }}
      >
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              fontSize: "0.875rem",
              fontWeight: "bold",
              whiteSpace: "nowrap",
              overflow: "hidden",
              textOverflow: "ellipsis",
            }}
          >
            Unit file &amp; dependencies
          </div>
        </div>

        <button
          onClick={onClose}
          style={{
            background: "none",
            border: "none",
            cursor: "pointer",
            padding: 4,
            borderRadius: 4,
            color: "var(--mui-palette-text-secondary)",
            display: "flex",
            alignItems: "center",
            flexShrink: 0,
          }}
        >
          <CloseIcon fontSize="small" />
        </button>
      </div>

      {/* Info rows */}
      <div style={{ flex: 1 }}>
        <Row label="Path" noBorder>
          {isPending ? (
            <div
              style={{
                height: 18,
                width: "80%",
                borderRadius: 4,
                backgroundColor: "var(--mui-palette-action-hover)",
              }}
            />
          ) : (
            <span
              style={{
                fontSize: "0.8rem",
                fontWeight: 500,
                wordBreak: "break-all",
              }}
            >
              {fragmentPath || "—"}
            </span>
          )}
        </Row>

        {/* Dependency rows */}
        {!isPending &&
          DEP_FIELDS.map(({ label, key }) => {
            const items = toStringArray(info?.[key]);
            if (!items.length) return null;
            return (
              <Row key={key} label={label}>
                <span
                  style={{
                    fontSize: "0.75rem",
                    fontWeight: 500,
                    wordBreak: "break-word",
                  }}
                >
                  {items.join(", ")}
                </span>
              </Row>
            );
          })}
      </div>
    </FrostedCard>
  );
};

export default ServiceDetailPanel;
