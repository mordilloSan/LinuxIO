import type { NetworkInterface } from "@/api";

export const getEgressNICOptions = (
  data: NetworkInterface[] | undefined,
): { name: string; label: string }[] => {
  if (!Array.isArray(data)) return [];

  return data
    .filter(
      (nic) =>
        (nic.type === "ethernet" || nic.type === "wifi") &&
        nic.mac &&
        !nic.name.startsWith("veth") &&
        !nic.name.startsWith("docker") &&
        !nic.name.startsWith("br-"),
    )
    .map((nic) => {
      const ip =
        Array.isArray(nic.ipv4) && nic.ipv4.length > 0
          ? nic.ipv4[0]
          : "disconnected";
      return {
        name: nic.name,
        label: `${nic.name} (${ip})`,
      };
    });
};
