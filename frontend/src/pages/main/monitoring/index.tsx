import React from "react";

import { useAppTheme } from "@/theme";

const grafanaProxyPath = "/proxy/grafana/";

const MonitoringPage: React.FC = () => {
  const theme = useAppTheme();

  return (
    <div
      style={{
        display: "flex",
        minHeight: "calc(100dvh - 220px)",
      }}
    >
      <div
        style={{
          flex: 1,
          minHeight: 720,
          borderRadius: theme.shape.borderRadius * 2,
          overflow: "hidden",
          border: "1px solid var(--app-palette-divider)",
          background: theme.palette.background.paper,
        }}
      >
        <iframe
          title="Grafana Monitoring"
          src={grafanaProxyPath}
          style={{
            width: "100%",
            height: "100%",
            border: 0,
            display: "block",
            background: theme.palette.background.paper,
          }}
          referrerPolicy="same-origin"
        />
      </div>
    </div>
  );
};

export default MonitoringPage;
