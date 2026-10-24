import { useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { toast } from "sonner";

import { ASSISTANT_AGENTS, isAssistantAgentId } from "@/api/acp/agents";
import {
  type AssistantProbe,
  authenticateAssistant,
  logoutAssistant,
  probeAssistant,
} from "@/api/acp/probe";
import { SettingsSectionShell } from "@/components/settings/SettingsSectionForm";
import {
  SectionCard,
  SettingsNote,
  StatusMetric,
} from "@/components/settings/SettingsSectionPrimitives";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppSelect from "@/components/ui/AppSelect";
import { useAssistantSettings, useConfig } from "@/hooks/useConfig";

export default function AssistantSettingsSection() {
  const { agent } = useAssistantSettings();
  const { updateConfig } = useConfig();
  const navigate = useNavigate();
  const [attempt, setAttempt] = useState(0);
  const [result, setResult] = useState<{
    key: string;
    probe: AssistantProbe;
  } | null>(null);
  const probeKey = `${agent}:${attempt}`;
  // A result belongs to the agent/attempt it was fetched for; anything else is
  // stale and the probe counts as still running.
  const probe = result?.key === probeKey ? result.probe : null;
  const probing = agent !== "" && probe === null;
  const runProbe = () => setAttempt((count) => count + 1);

  useEffect(() => {
    if (!agent) return;
    const controller = new AbortController();
    void probeAssistant(agent, undefined, controller.signal).then((next) => {
      if (!controller.signal.aborted) setResult({ key: probeKey, probe: next });
    });
    return () => controller.abort();
  }, [agent, probeKey]);

  const openLogin = (args: readonly string[] | undefined) => {
    if (!agent) return;
    void navigate({
      to: "/terminal",
      state: { terminalLogin: { agent, args: [...(args ?? [])] } },
    });
  };

  const authenticate = (methodId: string) => {
    if (!agent) return;
    void authenticateAssistant(agent, methodId)
      .then(runProbe)
      .catch((error: unknown) =>
        toast.error(error instanceof Error ? error.message : "Login failed"),
      );
  };

  const logOut = () => {
    if (!agent) return;
    void logoutAssistant(agent)
      .then(() => {
        toast.success("Logged out.");
        runProbe();
      })
      .catch((error: unknown) =>
        toast.error(error instanceof Error ? error.message : "Log out failed"),
      );
  };

  const loggedIn = probe?.status === "logged_in";
  const statusLabel = probing
    ? "Checking login…"
    : loggedIn
      ? "Logged in"
      : probe?.status === "auth_required"
        ? "Not logged in"
        : "Unreachable";
  const statusColor = loggedIn
    ? "var(--app-palette-success-main)"
    : probe?.status === "auth_required"
      ? "var(--app-palette-warning-main)"
      : probe?.status === "unknown"
        ? "var(--app-palette-error-main)"
        : undefined;

  return (
    <SettingsSectionShell
      onRefresh={runProbe}
      refreshAriaLabel="Check login again"
      refreshDisabled={!agent}
      refreshing={probing}
      subtitle="Coding agent used by the assistant"
      title="Assistant"
    >
      <SectionCard
        icon="mdi:robot-outline"
        subtitle="Agent CLI the assistant talks to"
        title="Agent"
      >
        <AppSelect
          fullWidth
          label="Agent"
          onChange={(event) => {
            const value = event.target.value;
            updateConfig({
              assistant: { agent: isAssistantAgentId(value) ? value : "" },
            });
          }}
          value={agent}
        >
          <option value="">Not chosen</option>
          {ASSISTANT_AGENTS.map((entry) => (
            <option key={entry.id} value={entry.id}>
              {entry.label}
            </option>
          ))}
        </AppSelect>
      </SectionCard>

      {agent ? (
        <SectionCard
          icon="mdi:account-key-outline"
          subtitle="Login state reported by the agent"
          title="Login"
        >
          <div
            style={{
              display: "flex",
              flexDirection: "column",
              gap: "var(--app-space-6)",
            }}
          >
            <StatusMetric
              detail={
                probe?.status === "logged_in"
                  ? probe.authStatus?.account?.email
                  : undefined
              }
              label="Status"
              statusColor={statusColor}
              value={
                probe?.status === "logged_in" && probe.authStatus
                  ? `${statusLabel}: ${probe.authStatus.label}`
                  : statusLabel
              }
            />
            {probe?.status === "logged_in" && probe.canLogout ? (
              <div>
                <AppButton onClick={logOut} variant="outlined">
                  Log out
                </AppButton>
              </div>
            ) : null}
            {probe?.status === "auth_required" ? (
              <>
                {probe.authMethods.length > 0 ? (
                  <div
                    style={{
                      display: "flex",
                      flexWrap: "wrap",
                      gap: "var(--app-space-6)",
                    }}
                  >
                    {probe.authMethods.map((method) =>
                      "type" in method && method.type === "terminal" ? (
                        <AppButton
                          key={method.id}
                          onClick={() => openLogin(method.args)}
                          variant="contained"
                        >
                          {method.name}
                        </AppButton>
                      ) : (
                        <AppButton
                          key={method.id}
                          onClick={() => authenticate(method.id)}
                          variant="outlined"
                        >
                          {method.name}
                        </AppButton>
                      ),
                    )}
                  </div>
                ) : (
                  <SettingsNote title="No login method advertised">
                    Log in from the Terminal page using the agent&apos;s own
                    CLI.
                  </SettingsNote>
                )}
              </>
            ) : null}
            {probe?.status === "unknown" ? (
              <AppAlert severity="error">
                Could not reach the agent
                {probe.error ? `: ${probe.error}` : ""}.
              </AppAlert>
            ) : null}
          </div>
        </SectionCard>
      ) : null}
    </SettingsSectionShell>
  );
}
