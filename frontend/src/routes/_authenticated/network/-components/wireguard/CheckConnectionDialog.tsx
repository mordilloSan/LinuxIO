import { Icon } from "@iconify/react";

import {
  getMutationErrorMessage,
  type WireGuardCheckResult,
  type WireGuardCheckStep,
  type WireGuardCheckStepID,
  type WireGuardCheckStatus,
} from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppTypography from "@/components/ui/AppTypography";

// What each step proves, in the order the check reports them. Keyed by the
// generated union, so typecheck fails when the backend adds or drops a step.
const STEPS: Record<
  WireGuardCheckStepID,
  {
    title: string;
    failed: (step: WireGuardCheckStep, port: number) => string;
  }
> = {
  handshake: {
    title: "Connects to the server",
    failed: (_step, port) =>
      `The server did not answer a handshake on port ${port}. A local firewall may be blocking it.`,
  },
  tunnel: {
    title: "Traffic passes through the tunnel",
    failed: () =>
      "The server's own tunnel address did not answer through the tunnel. A firewall on this server may be dropping what comes in on the interface.",
  },
  beyond: {
    title: "Reaches the network behind the server",
    failed: () =>
      "Peers connect but reach nothing beyond this server: IPv4 forwarding, the NAT rule or the firewall's forward rules are in the way.",
  },
  dns: {
    title: "Names resolve",
    failed: (step) =>
      `The DNS server ${step.target ?? ""} did not answer through the tunnel, so clients would connect but not open websites by name.`,
  },
};

const STEP_ORDER = Object.keys(STEPS) as WireGuardCheckStepID[];

const STATUS: Record<
  WireGuardCheckStatus,
  { color: string; icon: string; label: string }
> = {
  ok: {
    color: "var(--app-palette-success-main)",
    icon: "mdi:check-circle",
    label: "Passed",
  },
  failed: {
    color: "var(--app-palette-error-main)",
    icon: "mdi:close-circle",
    label: "Failed",
  },
  skipped: {
    color: "var(--app-palette-text-secondary)",
    icon: "mdi:minus-circle-outline",
    label: "Skipped",
  },
};

const plural = (count: number) => `${count} step${count === 1 ? "" : "s"}`;

/** One line that sums up a result: works, failed, or could not be checked. */
const verdict = (
  result: WireGuardCheckResult,
): { severity: "success" | "error" | "info"; text: string } => {
  const failed = result.steps.filter((step) => step.status === "failed");
  const skipped = result.steps.filter((step) => step.status === "skipped");
  if (failed.length > 0) {
    return {
      severity: "error",
      text: `${plural(failed.length)} of ${result.steps.length} failed: clients would not get everything this VPN should give them.`,
    };
  }
  if (skipped.length > 0) {
    return {
      severity: "info",
      text: `Nothing failed, but ${plural(skipped.length)} could not be checked.`,
    };
  }
  return {
    severity: "success",
    text: "This VPN works: a client connects, reaches your network and resolves names.",
  };
};

const sentence = (text: string) =>
  text ? text.charAt(0).toUpperCase() + text.slice(1) + "." : "";

interface CheckConnectionDialogProps {
  error: Error | null;
  interfaceName: string;
  onClose: () => void;
  onRunAgain: () => void;
  open: boolean;
  port: number;
  result: WireGuardCheckResult | undefined;
  running: boolean;
}

/**
 * The result of wireguard.check_interface. A check takes a few seconds and
 * reports here, so the dialog stays held while it runs (docs/jobs.md).
 */
const CheckConnectionDialog = ({
  error,
  interfaceName,
  onClose,
  onRunAgain,
  open,
  port,
  result,
  running,
}: CheckConnectionDialogProps) => {
  const steps = new Map(result?.steps.map((step) => [step.id, step]));
  const summary = result && !running ? verdict(result) : undefined;

  return (
    <GeneralDialog
      aria-busy={running}
      disableEscapeKeyDown={running}
      fullWidth
      maxWidth="sm"
      onClose={running ? undefined : onClose}
      open={open}
    >
      <AppDialogTitle>Check connection of {interfaceName}</AppDialogTitle>
      <AppDialogContent>
        <div className="app-dialog-fields">
          {/* Appears when a run ends, so it is announced (role="alert"). */}
          {summary && (
            <AppAlert severity={summary.severity}>{summary.text}</AppAlert>
          )}
          <ul
            aria-label="Connection check"
            style={{ listStyle: "none", margin: 0, padding: 0 }}
          >
            {STEP_ORDER.map((id) => {
              const step = running ? undefined : steps.get(id);
              const status = step ? STATUS[step.status] : undefined;
              const explanation =
                step?.status === "failed"
                  ? STEPS[id].failed(step, port)
                  : step?.status === "skipped"
                    ? `Skipped: ${sentence(step.detail ?? "")}`
                    : "";
              const secondary =
                step?.status === "ok"
                  ? step.target
                  : step?.status === "failed"
                    ? step.detail
                    : undefined;
              return (
                <li
                  key={id}
                  style={{
                    alignItems: "flex-start",
                    display: "flex",
                    gap: 12,
                    paddingBlock: 8,
                  }}
                >
                  <span
                    aria-label={
                      status?.label ?? (running ? "Checking" : "Not run")
                    }
                    role="img"
                    style={{
                      display: "inline-flex",
                      flexShrink: 0,
                      height: 22,
                      justifyContent: "center",
                      width: 22,
                    }}
                  >
                    {status ? (
                      <Icon
                        aria-hidden
                        color={status.color}
                        height={22}
                        icon={status.icon}
                        width={22}
                      />
                    ) : running ? (
                      <AppCircularProgress aria-hidden size={18} />
                    ) : (
                      <Icon
                        aria-hidden
                        color="var(--app-palette-text-secondary)"
                        height={22}
                        icon="mdi:circle-outline"
                        width={22}
                      />
                    )}
                  </span>
                  <div style={{ minWidth: 0 }}>
                    <AppTypography variant="body1">
                      {STEPS[id].title}
                    </AppTypography>
                    {explanation && (
                      <AppTypography color="text.secondary" variant="body2">
                        {explanation}
                      </AppTypography>
                    )}
                    {secondary && (
                      <AppTypography
                        color="text.secondary"
                        style={{
                          fontFamily: "var(--app-font-mono)",
                          overflowWrap: "anywhere",
                        }}
                        variant="caption"
                      >
                        {secondary}
                      </AppTypography>
                    )}
                  </div>
                </li>
              );
            })}
          </ul>
          {error && !running && (
            <AppAlert severity="error">
              {getMutationErrorMessage(error, "The check could not run")}
            </AppAlert>
          )}
          <AppTypography color="text.secondary" variant="body2">
            Checked from this server. It cannot tell whether your router
            forwards UDP port {port} to it.
          </AppTypography>
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={running} onClick={onClose}>
          Close
        </AppButton>
        <AppButton disabled={running} onClick={onRunAgain} variant="contained">
          {running ? "Checking…" : "Run again"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default CheckConnectionDialog;
