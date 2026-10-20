import type { CSSProperties } from "react";

import type { VMPreflight } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";

import PreflightSummary from "./PreflightSummary";

const preflightCardStyle: CSSProperties = {
  display: "flex",
  flexDirection: "column",
  gap: "var(--app-space-8)",
  padding: "var(--app-space-12)",
};

const messageListStyle: CSSProperties = {
  margin: 0,
  paddingLeft: 18,
};

export function VMPreflightCard({ preflight }: { preflight?: VMPreflight }) {
  return (
    <FrostedCard style={preflightCardStyle}>
      <PreflightSummary preflight={preflight} />
      {preflight && (preflight.warnings ?? []).length > 0 && (
        <AppAlert severity="warning">
          <AppAlertTitle>Preflight Warnings</AppAlertTitle>
          <ul style={messageListStyle}>
            {(preflight.warnings ?? []).map((warning) => (
              <li key={warning}>{warning}</li>
            ))}
          </ul>
        </AppAlert>
      )}
      {preflight && (preflight.errors ?? []).length > 0 && (
        <AppAlert severity="error">
          <AppAlertTitle>Preflight Errors</AppAlertTitle>
          <ul style={messageListStyle}>
            {(preflight.errors ?? []).map((error) => (
              <li key={error}>{error}</li>
            ))}
          </ul>
        </AppAlert>
      )}
    </FrostedCard>
  );
}
