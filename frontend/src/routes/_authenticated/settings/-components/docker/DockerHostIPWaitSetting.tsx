import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { useState, type CSSProperties } from "react";
import { toast } from "sonner";

import { linuxio } from "@/api";
import { cardBodyToggleProps } from "@/components/cards/cardBodyToggle";
import FrostedCard from "@/components/cards/FrostedCard";
import { useSettingsDraft } from "@/components/settings/SettingsSectionForm";
import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import useAuth from "@/hooks/useAuth";
import { useConfig, useDockerSettings } from "@/hooks/useConfig";
import { validateHostIPs } from "@/utils/hostIp";

// The 30 s is the limit the backend writes into the drop-in
// (dockerHostIPWaitSeconds).
const NOT_ASSIGNED_NOTE =
  "Not assigned on this host now. Docker waits 30 s for it at every boot.";

interface HostIPWaitDraft {
  enabled: boolean;
  hostIPs: string[];
}

// Off sends the switch alone and keeps the stored list, so rows edited
// before switching off are nothing to save. Both lists are compared in the
// form a save stores, so a list just sent is not dirty while it is saved, and
// neither is a stored one that was written by hand in another spelling.
const storedForm = (hostIPs: string[]) =>
  JSON.stringify(validateHostIPs(hostIPs).hostIPs);
const isSameSetting = (draft: HostIPWaitDraft, saved: HostIPWaitDraft) =>
  draft.enabled === saved.enabled &&
  (!draft.enabled || storedForm(draft.hostIPs) === storedForm(saved.hostIPs));

const iconStyle: CSSProperties = {
  display: "inline-flex",
  alignItems: "center",
  justifyContent: "center",
  flexShrink: 0,
  width: 36,
  height: 36,
  borderRadius: "var(--app-radius-md)",
  background: "var(--app-palette-action-hover)",
  color: "var(--app-palette-primary-main)",
};

/**
 * The "Wait for published host IPs" block of the Docker settings: a toggle and
 * a list of addresses, saved together. Nothing is stored until Save.
 */
const DockerHostIPWaitSetting = () => {
  const { privileged } = useAuth();
  const { updateConfig } = useConfig();
  const { waitForHostIPs, hostIPs: savedHostIPs } = useDockerSettings();
  const saved: HostIPWaitDraft = {
    enabled: waitForHostIPs,
    hostIPs: savedHostIPs,
  };
  const { draft, draftPatch, isDirty, patchKey, reset, resetSaved } =
    useSettingsDraft<HostIPWaitDraft, object>(saved, undefined, isSameSetting);
  const [errorTexts, setErrorTexts] = useState<string[]>([]);

  const enabled = draft?.enabled ?? saved.enabled;
  const { data: detected } = useQuery({
    ...linuxio.docker.list_host_ips,
    enabled,
  });
  const published = detected?.published ?? [];
  const assigned = detected ? new Set(detected.assigned) : null;

  // With nothing stored and nothing typed yet, the list starts from the
  // published addresses the host holds now. An address no interface holds is
  // offered below instead, with its note.
  const prefill =
    savedHostIPs.length === 0
      ? published.filter((ip) => assigned?.has(ip))
      : [];
  const listed =
    draftPatch.hostIPs ?? (savedHostIPs.length > 0 ? savedHostIPs : prefill);
  // The list always shows at least one (empty) row.
  const rows = listed.length > 0 ? listed : [""];
  const { hostIPs: canonicalRows, errors: rowErrors } = validateHostIPs(rows);
  const notListed = published.filter((ip) => !canonicalRows.includes(ip));
  const locked = !privileged;

  const setRows = (next: string[]) => patchKey("hostIPs", next);

  const handleRowChange = (index: number, value: string) => {
    setRows(rows.map((row, rowIndex) => (rowIndex === index ? value : row)));
    setErrorTexts((prev) =>
      prev.map((text, rowIndex) => (rowIndex === index ? "" : text)),
    );
  };

  const handleRemoveRow = (index: number) => {
    setRows(rows.filter((_, rowIndex) => rowIndex !== index));
    setErrorTexts((prev) => prev.filter((_, rowIndex) => rowIndex !== index));
  };

  const handleAddDetected = (ip: string) => {
    setRows([...rows.filter((row) => row.trim() !== ""), ip]);
    setErrorTexts([]);
  };

  const handleReset = () => {
    reset();
    setErrorTexts([]);
  };

  // The draft is cleared only once the bridge confirms: a refused save is
  // read back, and the form then still holds what was typed.
  const handleSave = () => {
    if (!enabled) {
      // Off keeps the stored list, so only the switch is sent.
      updateConfig({ docker: { waitForHostIPs: false } }, () => {
        resetSaved(draftPatch);
        toast.success("Docker no longer waits for host IPs.");
      });
      return;
    }
    const { hostIPs, errors } = validateHostIPs(rows);
    if (errors.some(Boolean)) {
      setErrorTexts(errors);
      return;
    }
    updateConfig({ docker: { waitForHostIPs: true, hostIPs } }, () => {
      resetSaved(draftPatch);
      toast.success("Docker will wait for the listed host IPs.");
    });
  };

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--app-space-6)",
      }}
    >
      <FrostedCard
        hoverLift
        {...cardBodyToggleProps({
          checked: enabled,
          disabled: locked,
          onChange: (next) => patchKey("enabled", next),
        })}
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          gap: "var(--app-space-6)",
          opacity: locked ? 0.72 : 1,
          padding: 12,
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: "var(--app-space-6)",
            minWidth: 0,
          }}
        >
          <div style={iconStyle}>
            <Icon height={22} icon="mdi:lan-connect" width={22} />
          </div>
          <div style={{ minWidth: 0 }}>
            <AppTypography fontWeight={600} variant="body2">
              Wait for published host IPs
            </AppTypography>
            <AppTypography color="text.secondary" variant="caption">
              {locked
                ? "Privileged mode is required to change Docker startup ordering."
                : "Wait until the IP addresses that containers publish ports on are assigned before Docker starts."}
            </AppTypography>
          </div>
        </div>
        <AppSwitch
          aria-label="Wait for published host IPs"
          checked={enabled}
          disabled={locked}
          onChange={(_, checked) => patchKey("enabled", checked)}
        />
      </FrostedCard>

      {enabled ? (
        <>
          {rows.map((row, index) => {
            const errorText = errorTexts[index] ?? "";
            const notAssigned =
              assigned !== null &&
              rowErrors[index] === "" &&
              !assigned.has(canonicalRows[index]);
            return (
              <FrostedCard key={index} style={{ padding: 12 }}>
                <div
                  style={{
                    display: "flex",
                    alignItems: "flex-start",
                    gap: "var(--app-space-6)",
                  }}
                >
                  <div style={iconStyle}>
                    <Icon
                      height={22}
                      icon="mdi:ip-network-outline"
                      width={22}
                    />
                  </div>

                  <AppTextField
                    disabled={locked}
                    error={Boolean(errorText)}
                    fullWidth
                    helperText={
                      errorText ||
                      (notAssigned ? (
                        <AppTypography
                          color="warning"
                          component="span"
                          variant="caption"
                        >
                          {NOT_ASSIGNED_NOTE}
                        </AppTypography>
                      ) : undefined)
                    }
                    label={
                      index === 0 ? "IP address" : `IP address ${index + 1}`
                    }
                    onChange={(event) =>
                      handleRowChange(index, event.target.value)
                    }
                    placeholder="192.168.1.66"
                    style={{ flex: 1 }}
                    value={row}
                  />

                  {rows.length > 1 ? (
                    <AppIconButton
                      aria-label={`Remove IP address ${index + 1}`}
                      disabled={locked}
                      onClick={() => handleRemoveRow(index)}
                      size="small"
                      style={{ marginTop: 3 }}
                    >
                      <Icon height={16} icon="mdi:close" width={16} />
                    </AppIconButton>
                  ) : null}
                </div>
              </FrostedCard>
            );
          })}

          <FrostedCard hoverLift={!locked} style={{ padding: 0 }}>
            <AppButton
              color="inherit"
              disabled={locked}
              fullWidth
              onClick={() => setRows([...rows, ""])}
              style={{
                alignItems: "center",
                borderRadius: "inherit",
                display: "flex",
                gap: "var(--app-space-6)",
                justifyContent: "flex-start",
                minWidth: 0,
                opacity: locked ? 0.65 : 1,
                padding: 12,
                textAlign: "left",
              }}
            >
              <div style={iconStyle}>
                <Icon height={22} icon="mdi:plus" width={22} />
              </div>
              <div>
                <AppTypography fontWeight={600} variant="body2">
                  Add IP address
                </AppTypography>
                <AppTypography color="text.secondary" variant="caption">
                  Add another address Docker waits for.
                </AppTypography>
              </div>
            </AppButton>
          </FrostedCard>

          {notListed.length > 0 ? (
            <div
              style={{
                display: "flex",
                flexDirection: "column",
                gap: "var(--app-space-2)",
              }}
            >
              <AppTypography color="text.secondary" variant="caption">
                Detected on containers, not in the list:
              </AppTypography>
              {notListed.map((ip) => (
                <div
                  key={ip}
                  style={{
                    display: "flex",
                    alignItems: "center",
                    flexWrap: "wrap",
                    gap: "var(--app-space-4)",
                  }}
                >
                  <AppTypography variant="body2">{ip}</AppTypography>
                  <AppButton
                    aria-label={`Add ${ip}`}
                    disabled={locked}
                    onClick={() => handleAddDetected(ip)}
                    size="small"
                  >
                    Add
                  </AppButton>
                  {assigned?.has(ip) ? null : (
                    <AppTypography color="warning" variant="caption">
                      {NOT_ASSIGNED_NOTE}
                    </AppTypography>
                  )}
                </div>
              ))}
            </div>
          ) : null}
        </>
      ) : null}

      {enabled || isDirty ? (
        <div
          style={{
            display: "flex",
            justifyContent: "flex-end",
            gap: "var(--app-space-6)",
            paddingTop: "var(--app-space-2)",
          }}
        >
          <AppButton disabled={!isDirty} onClick={handleReset}>
            Reset
          </AppButton>
          <AppButton
            disabled={!isDirty || locked}
            onClick={handleSave}
            variant="contained"
          >
            Save
          </AppButton>
        </div>
      ) : null}
    </div>
  );
};

export default DockerHostIPWaitSetting;
