import CloseIcon from "@mui/icons-material/Close";
import {
  Box,
  IconButton,
  Skeleton,
  Typography,
} from "@mui/material";
import React from "react";

import { linuxio } from "@/api";
import type { Service } from "@/api";
import FrostedCard from "@/components/cards/RootCard";

interface ServiceDetailPanelProps {
  service: Service;
  onClose: () => void;
}

const labelSx = {
  textTransform: "uppercase" as const,
  letterSpacing: "0.06em",
  fontSize: "0.6rem",
  color: "text.secondary",
  flexShrink: 0,
  width: 90,
  pt: 0.3,
};

const sectionLabelSx = {
  textTransform: "uppercase" as const,
  letterSpacing: "0.06em",
  fontSize: "0.6rem",
  color: "text.secondary",
  display: "block",
  mb: 0.75,
};

const Row: React.FC<{ label: string; children: React.ReactNode }> = ({
  label,
  children,
}) => (
  <Box
    sx={{
      display: "flex",
      gap: 2,
      py: 0.75,
      borderBottom: "1px solid",
      borderColor: "divider",
      "&:last-child": { borderBottom: "none" },
      alignItems: "flex-start",
    }}
  >
    <Typography variant="caption" sx={labelSx}>
      {label}
    </Typography>
    <Box sx={{ flex: 1, minWidth: 0 }}>{children}</Box>
  </Box>
);

const formatBytes = (val: unknown): string => {
  const b = Number(val ?? 0);
  if (!b || b > 1e18) return "—";
  if (b < 1024) return `${b} B`;
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} kB`;
  if (b < 1024 * 1024 * 1024) return `${(b / (1024 * 1024)).toFixed(1)} MB`;
  return `${(b / (1024 * 1024 * 1024)).toFixed(2)} GB`;
};

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
  const mainPid = Number(info?.MainPID ?? 0);
  const memory = formatBytes(info?.MemoryCurrent);

  return (
    <FrostedCard sx={{ p: 3 }}>
      {/* Header*/}
      <Box
        sx={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "flex-start",
          mb: 1.5,
          gap: 1,
        }}
      >
        <Box sx={{ flex: 1, minWidth: 0 }}>
                                  <Typography
                                    variant="body2"
                                    fontWeight="bold"
                                    noWrap
                                    sx={{ minWidth: 0 }}
                                  >
            Relationships
          </Typography>
          <Typography
            variant="caption"
            color="text.secondary"
            noWrap
            sx={{ display: "block" }}
          >
            teste
          </Typography>
        </Box>

        <IconButton onClick={onClose} size="small">
          <CloseIcon fontSize="small" />
        </IconButton>
      </Box>

      {/* Info rows */}
      <Box>
        {mainPid > 0 && (
          <Row label="PID">
            <Typography variant="body2" fontWeight={500}>
              {mainPid}
            </Typography>
          </Row>
        )}

        {memory !== "—" && (
          <Row label="Memory">
            <Typography variant="body2" fontWeight={500}>
              {memory}
            </Typography>
          </Row>
        )}

        <Row label="Path">
          {isPending ? (
            <Skeleton width="80%" height={20} />
          ) : (
            <Typography
              variant="body2"
              fontWeight={500}
              sx={{ wordBreak: "break-all", fontSize: "0.8rem" }}
            >
              {fragmentPath || "—"}
            </Typography>
          )}
        </Row>

        {/* Dependency rows */}
        {!isPending &&
          DEP_FIELDS.map(({ label, key }) => {
            const items = toStringArray(info?.[key]);
            if (!items.length) return null;
            return (
              <Row key={key} label={label}>
                <Typography
                  variant="body2"
                  fontWeight={500}
                  sx={{ wordBreak: "break-word" }}
                >
                  {items.join(", ")}
                </Typography>
              </Row>
            );
          })}
      </Box>



    </FrostedCard>
  );
};

export default ServiceDetailPanel;
