import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";

import { linuxio, type ContainerPortBinding } from "@/api";
import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";
import useAuth from "@/hooks/useAuth";
import { useConfig, useDockerSettings } from "@/hooks/useConfig";
import { validateHostIPs } from "@/utils/hostIp";

/**
 * The specific host addresses the ports are published on, in the form the
 * config stores them: no "every address", no loopback, each one once.
 */
function publishedHostIPs(ports: readonly ContainerPortBinding[]): string[] {
  const { hostIPs, errors } = validateHostIPs(
    ports.filter((port) => port.hostPort && port.hostIp).map((p) => p.hostIp),
  );
  return hostIPs.filter((_, index) => errors[index] === "");
}

/**
 * Under a container's ports: says whether Docker waits at boot for the host
 * address they are published on, and offers to add it to the "Wait for
 * published host IPs" list of the Docker settings. It is a shortcut into that
 * setting, which stays the one place the list is kept.
 */
const ContainerHostIPWaitHint = ({
  ports,
}: {
  ports: readonly ContainerPortBinding[];
}) => {
  const { privileged } = useAuth();
  const { updateConfig } = useConfig();
  const { waitForHostIPs, hostIPs } = useDockerSettings();

  const published = publishedHostIPs(ports);
  const waitedFor = published.filter(
    (ip) => waitForHostIPs && hostIPs.includes(ip),
  );
  const notWaitedFor = published.filter((ip) => !waitedFor.includes(ip));
  const { data: detected } = useQuery({
    ...linuxio.docker.list_host_ips,
    enabled: privileged && notWaitedFor.length > 0,
  });

  // The setting needs a privileged session, so nobody else is shown it.
  if (!privileged) return null;
  // Only an address the bridge reports as both published and held now is
  // offered. Waiting for one the server does not hold (a typo, or the gateway
  // of a Docker network, which Docker assigns itself after it starts and the
  // bridge leaves out of both lists) would cost 30 s at every boot.
  const offered = notWaitedFor.filter(
    (ip) => detected?.published.includes(ip) && detected.assigned.includes(ip),
  );
  if (waitedFor.length === 0 && offered.length === 0) return null;

  const waitFor = (ip: string) => {
    // The list is kept while the setting is off. Turning the setting on from
    // here must not bring back a stored address the server no longer holds,
    // which nobody is looking at on this page.
    const kept = hostIPs.filter(
      (stored) =>
        stored !== ip &&
        (waitForHostIPs || detected?.assigned.includes(stored)),
    );
    const next = [...kept, ip];
    updateConfig({ docker: { waitForHostIPs: true, hostIPs: next } }, () =>
      toast.success(`Docker will wait for ${next.join(", ")} at boot.`),
    );
  };

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--app-space-2)",
      }}
    >
      {waitedFor.map((ip) => (
        <AppTypography color="text.secondary" key={ip} variant="caption">
          Docker waits for {ip} at boot.
        </AppTypography>
      ))}
      {offered.map((ip) => (
        <div
          key={ip}
          style={{
            display: "flex",
            alignItems: "center",
            flexWrap: "wrap",
            gap: "var(--app-space-4)",
          }}
        >
          <AppTypography color="text.secondary" variant="caption">
            Docker does not wait for {ip} at boot. If Docker starts first, this
            container fails.
          </AppTypography>
          <AppButton
            aria-label={`Make Docker wait for ${ip} at boot`}
            onClick={() => waitFor(ip)}
            size="small"
          >
            Wait for it
          </AppButton>
        </div>
      ))}
    </div>
  );
};

export default ContainerHostIPWaitHint;
