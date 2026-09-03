import { Fragment, useState, type ReactNode } from "react";

import type { ContainerInspectInfo, DockerNetwork } from "@/api";
import { DetailRow } from "@/components/cards/UnitInfoPanelCard";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import AppDivider from "@/components/ui/AppDivider";
import AppTypography from "@/components/ui/AppTypography";
import { getContainerStatusColor } from "@/constants/statusColors";

const Section = ({
  children,
  title,
}: {
  children: ReactNode;
  title: string;
}) => (
  <section
    style={{
      display: "flex",
      flexDirection: "column",
      gap: "var(--app-space-4)",
      minWidth: 0,
    }}
  >
    <AppTypography fontWeight={700} variant="subtitle2">
      {title}
    </AppTypography>
    {children}
  </section>
);

const TextValue = ({ children }: { children: ReactNode }) => (
  <AppTypography component="span" fontWeight={500} variant="caption">
    {children === "" || children === null || children === undefined
      ? "—"
      : children}
  </AppTypography>
);

const CommandValue = ({ value }: { value?: string[] }) => {
  const text = value?.length ? value.join(" ") : "Image default";
  return (
    <AppTypography
      component="span"
      copyText={text}
      noWrap
      style={{ fontFamily: "var(--app-font-mono)" }}
      title={text}
      variant="caption"
    >
      {text}
    </AppTypography>
  );
};

const networkLabel = (name: string, driver?: string) => {
  if (name === "bridge") return "Default bridge";
  if (name === "host") return "Host network";
  if (name === "none") return "No network";
  return driver ? `Custom network ${driver}` : "Custom network";
};

export type ContainerInspectSection =
  | "overview"
  | "configuration"
  | "environment"
  | "labels"
  | "ports"
  | "mounts"
  | "networks";

const ALL_SECTIONS: ContainerInspectSection[] = [
  "overview",
  "configuration",
  "environment",
  "labels",
  "ports",
  "mounts",
  "networks",
];

const ContainerInspectSections = ({
  inspect,
  networks = [],
  sections = ALL_SECTIONS,
}: {
  inspect: ContainerInspectInfo;
  networks?: DockerNetwork[];
  sections?: ContainerInspectSection[];
}) => {
  const [showEnvironment, setShowEnvironment] = useState(false);
  const restartPolicy =
    inspect.restartPolicy.name === "on-failure" &&
    inspect.restartPolicy.maximumRetryCount > 0
      ? `${inspect.restartPolicy.name} (${inspect.restartPolicy.maximumRetryCount} retries)`
      : inspect.restartPolicy.name || "no";
  const visibleSections = ALL_SECTIONS.filter((section) =>
    sections.includes(section),
  );
  const wildcardPorts = new Set<string>();
  const visiblePorts = inspect.ports?.filter((port) => {
    if (port.hostIp !== "0.0.0.0" && port.hostIp !== "::") return true;
    const key = `${port.protocol}:${port.hostPort}:${port.containerPort}`;
    if (wildcardPorts.has(key)) return false;
    wildcardPorts.add(key);
    return true;
  });
  const renderSection = (
    section: ContainerInspectSection,
    content: ReactNode,
  ) => {
    const index = visibleSections.indexOf(section);
    if (index === -1) return null;
    return (
      <Fragment key={section}>
        {content}
        {index < visibleSections.length - 1 && <AppDivider />}
      </Fragment>
    );
  };

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--app-space-12)",
        minWidth: 0,
      }}
    >
      {renderSection(
        "overview",
        <Section title="Overview and health">
          <div>
            <DetailRow label="State" noBorder>
              <Chip
                color={getContainerStatusColor(inspect.state.status)}
                label={inspect.state.status || "unknown"}
                size="xsmall"
                variant="soft"
              />
            </DetailRow>
            <DetailRow label="Health">
              <TextValue>
                {inspect.health?.status || "Not configured"}
              </TextValue>
            </DetailRow>
            {inspect.health && (
              <DetailRow label="Failing streak">
                <TextValue>{inspect.health.failingStreak}</TextValue>
              </DetailRow>
            )}
            <DetailRow label="Created">
              <TextValue>{inspect.created}</TextValue>
            </DetailRow>
            <DetailRow label="Restart count">
              <TextValue>{inspect.restartCount}</TextValue>
            </DetailRow>
            <DetailRow label="Exit code">
              <TextValue>{inspect.state.exitCode}</TextValue>
            </DetailRow>
            {inspect.state.error && (
              <DetailRow label="Last error">
                <TextValue>{inspect.state.error}</TextValue>
              </DetailRow>
            )}
          </div>
        </Section>,
      )}

      {renderSection(
        "configuration",
        <Section title="Configuration">
          <div>
            <DetailRow label="Image" noBorder>
              <TextValue>{inspect.image}</TextValue>
            </DetailRow>
            <DetailRow label="Command">
              <CommandValue value={inspect.command} />
            </DetailRow>
            <DetailRow label="Entrypoint">
              <CommandValue value={inspect.entrypoint} />
            </DetailRow>
            <DetailRow label="Restart policy">
              <TextValue>{restartPolicy}</TextValue>
            </DetailRow>
            <DetailRow label="User">
              <TextValue>{inspect.user || "Image default"}</TextValue>
            </DetailRow>
            <DetailRow label="Working directory">
              <TextValue>
                {inspect.workingDirectory || "Image default"}
              </TextValue>
            </DetailRow>
          </div>
        </Section>,
      )}

      {renderSection(
        "environment",
        <Section title="Environment variables">
          <div style={{ display: "flex", justifyContent: "flex-end" }}>
            <AppButton
              aria-pressed={showEnvironment}
              onClick={() => setShowEnvironment((visible) => !visible)}
              size="small"
              variant="outlined"
            >
              {showEnvironment ? "Hide values" : "Show values"}
            </AppButton>
          </div>
          {inspect.environment && inspect.environment.length > 0 ? (
            <div>
              {inspect.environment.map((variable, index) => (
                <DetailRow
                  key={`${variable.name}-${index}`}
                  label={variable.name || "Unnamed"}
                  noBorder={index === 0}
                  split
                >
                  <AppTypography
                    component="div"
                    copyText={showEnvironment ? variable.value : undefined}
                    noWrap
                    style={{
                      fontFamily: "var(--app-font-mono)",
                      textAlign: "right",
                    }}
                    title={showEnvironment ? variable.value : "Value hidden"}
                    variant="caption"
                  >
                    {showEnvironment ? variable.value || "(empty)" : "••••••••"}
                  </AppTypography>
                </DetailRow>
              ))}
            </div>
          ) : (
            <AppTypography color="text.secondary" variant="body2">
              No environment variables.
            </AppTypography>
          )}
        </Section>,
      )}

      {renderSection(
        "labels",
        <Section title="Labels">
          <div
            style={{
              alignItems: "flex-start",
              display: "flex",
              flexDirection: "column",
              gap: 6,
            }}
          >
            {inspect.labels && Object.keys(inspect.labels).length > 0 ? (
              Object.entries(inspect.labels)
                .sort(([left], [right]) => left.localeCompare(right))
                .map(([key, value]) => (
                  <Chip
                    key={key}
                    label={`${key}=${value}`}
                    size="xsmall"
                    title={`${key}=${value}`}
                    variant="soft"
                  />
                ))
            ) : (
              <AppTypography color="text.secondary" variant="body2">
                No labels.
              </AppTypography>
            )}
          </div>
        </Section>,
      )}

      {renderSection(
        "ports",
        <Section title="Ports">
          {visiblePorts && visiblePorts.length > 0 ? (
            <div>
              {visiblePorts.map((port, index) => {
                const value = port.hostPort
                  ? `${port.hostIp ? `${port.hostIp}:` : ""}${port.hostPort} → ${port.containerPort}`
                  : `${port.containerPort} (not published)`;
                return (
                  <DetailRow
                    key={`${port.containerPort}-${port.protocol}-${port.hostIp}-${port.hostPort}-${index}`}
                    label={port.protocol}
                    noBorder={index === 0}
                  >
                    <AppTypography
                      component="div"
                      copyText={`${port.protocol} ${value}`}
                      fontWeight={500}
                      noWrap
                      title={value}
                      variant="caption"
                    >
                      {value}
                    </AppTypography>
                  </DetailRow>
                );
              })}
            </div>
          ) : (
            <AppTypography color="text.secondary" variant="body2">
              No exposed ports.
            </AppTypography>
          )}
        </Section>,
      )}

      {renderSection(
        "mounts",
        <Section title="Mounts">
          {inspect.mounts && inspect.mounts.length > 0 ? (
            <div>
              {inspect.mounts.map((mount, index) => {
                const path = mount.Name || mount.Source || "—";
                const value = `${path} → ${mount.Destination}${mount.RW ? "" : " (read-only)"}`;
                return (
                  <DetailRow
                    key={`${mount.Source}-${mount.Destination}`}
                    label={mount.Type}
                    noBorder={index === 0}
                  >
                    <AppTypography
                      component="div"
                      copyText={`${mount.Type} ${value}`}
                      fontWeight={500}
                      noWrap
                      title={value}
                      variant="caption"
                    >
                      {value}
                    </AppTypography>
                  </DetailRow>
                );
              })}
            </div>
          ) : (
            <AppTypography color="text.secondary" variant="body2">
              No mounts.
            </AppTypography>
          )}
        </Section>,
      )}

      {renderSection(
        "networks",
        <Section title="Networks">
          {inspect.networks && Object.keys(inspect.networks).length > 0 ? (
            <div>
              {Object.entries(inspect.networks)
                .sort(([left], [right]) => left.localeCompare(right))
                .map(([name, endpoint], index) => {
                  const driver = networks.find(
                    (network) => network.Name === name,
                  )?.Driver;
                  return (
                    <div key={name}>
                      <DetailRow
                        label={networkLabel(name, driver)}
                        noBorder={index === 0}
                        split
                      >
                        <TextValue>{name}</TextValue>
                      </DetailRow>
                      <DetailRow label="IPv4" split>
                        <TextValue>{endpoint.IPAddress}</TextValue>
                      </DetailRow>
                      <DetailRow label="IPv6" split>
                        <TextValue>{endpoint.GlobalIPv6Address}</TextValue>
                      </DetailRow>
                    </div>
                  );
                })}
            </div>
          ) : (
            <AppTypography color="text.secondary" variant="body2">
              No networks attached.
            </AppTypography>
          )}
        </Section>,
      )}
    </div>
  );
};

export default ContainerInspectSections;
