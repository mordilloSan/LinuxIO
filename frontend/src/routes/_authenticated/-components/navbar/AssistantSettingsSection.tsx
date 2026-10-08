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
import FrostedCard from "@/components/cards/FrostedCard";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppSelect from "@/components/ui/AppSelect";
import AppTypography from "@/components/ui/AppTypography";
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

  return (
    <FrostedCard>
      <AppTypography variant="h6">Assistant</AppTypography>
      <AppSelect
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

      {agent ? (
        <div>
          {probing ? (
            <AppTypography color="text.secondary">
              Checking login…
            </AppTypography>
          ) : null}
          {probe?.status === "logged_in" ? (
            <>
              <AppTypography>
                Logged in
                {probe.authStatus ? `: ${probe.authStatus.label}` : ""}
              </AppTypography>
              {probe.authStatus?.account?.email ? (
                <AppTypography color="text.secondary" variant="caption">
                  {probe.authStatus.account.email}
                </AppTypography>
              ) : null}
              {probe.canLogout ? (
                <AppButton onClick={logOut} variant="outlined">
                  Log out
                </AppButton>
              ) : null}
            </>
          ) : null}
          {probe?.status === "auth_required" ? (
            <>
              <AppTypography>Not logged in.</AppTypography>
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
              {probe.authMethods.length === 0 ? (
                <AppTypography color="text.secondary" variant="caption">
                  This agent did not advertise a login method. Log in from the
                  Terminal page using the agent&apos;s own CLI.
                </AppTypography>
              ) : null}
            </>
          ) : null}
          {probe?.status === "unknown" ? (
            <AppAlert severity="error">
              Could not reach the agent{probe.error ? `: ${probe.error}` : ""}.
            </AppAlert>
          ) : null}
          {!probing ? (
            <AppButton onClick={runProbe}>Check again</AppButton>
          ) : null}
        </div>
      ) : null}
    </FrostedCard>
  );
}
