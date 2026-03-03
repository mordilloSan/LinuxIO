import TerminalIcon from "@mui/icons-material/Terminal";
import { FormControlLabel, Switch, Tooltip, useTheme } from "@mui/material";
import { alpha } from "@mui/material/styles";
import React from "react";

import { openServiceLogsStream } from "@/api";
import FrostedCard from "@/components/cards/RootCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import { useLogStream } from "@/hooks/useLogStream";

interface UnitLogsCardProps {
  unitName: string;
  title: string;
}

const UnitLogsCard: React.FC<UnitLogsCardProps> = ({ unitName, title }) => {
  const theme = useTheme();
  const { logs, isLoading, error, liveMode, setLiveMode, logsBoxRef } =
    useLogStream({
      open: true,
      createStream: (tail) => openServiceLogsStream(unitName, tail),
    });

  return (
    <FrostedCard sx={{ p: 3 }}>
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          marginBottom: 12,
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <TerminalIcon
            fontSize="small"
            style={{ color: "var(--mui-palette-text-secondary)" }}
          />
          <span style={{ fontSize: "0.875rem", fontWeight: 600 }}>{title}</span>
        </div>
        <Tooltip title={liveMode ? "Live streaming ON" : "Live streaming OFF"}>
          <FormControlLabel
            control={
              <Switch
                checked={liveMode}
                onChange={(_, value) => setLiveMode(value)}
                size="small"
              />
            }
            label="Live"
          />
        </Tooltip>
      </div>
      <div
        style={{
          position: "relative",
          backgroundColor: theme.codeBlock.background,
          color: theme.codeBlock.color,
          borderRadius: 4,
          overflow: "hidden",
        }}
      >
        {isLoading && (
          <div
            style={{
              position: "absolute",
              inset: 0,
              background: alpha(theme.codeBlock.background, 0.85),
              zIndex: 10,
            }}
          >
            <ComponentLoader />
          </div>
        )}
        {error && (
          <div style={{ color: "var(--mui-palette-error-main)", padding: 16 }}>
            {error}
          </div>
        )}
        <div
          ref={logsBoxRef}
          className="custom-scrollbar"
          style={{
            padding: 16,
            overflow: "auto",
            fontFamily: "Fira Mono, monospace",
            fontSize: "0.8rem",
            whiteSpace: "pre-wrap",
            wordBreak: "break-all",
            minHeight: 120,
            maxHeight: 340,
          }}
        >
          {!isLoading &&
            !error &&
            (logs || (
              <span
                style={{
                  color: "var(--mui-palette-text-secondary)",
                  fontSize: "0.75rem",
                }}
              >
                No logs available.
              </span>
            ))}
        </div>
      </div>
    </FrostedCard>
  );
};

export default UnitLogsCard;
