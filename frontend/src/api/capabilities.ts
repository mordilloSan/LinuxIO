/**
 * Single source of truth for system capabilities.
 *
 * Every capability lives in CAPABILITIES exactly once. Wire types, auth state
 * shape, reset helpers, and the capability-manager UI are all derived from it.
 * Adding a capability = adding one entry to this list.
 */

import type { CapabilitiesResponse as GeneratedCapabilitiesResponse } from "./generated/linuxio-types";

export interface CapabilityDef {
  /** Underlying dependency name (binary, package, or service). */
  dependency: string;
  /** One-line description shown under the label. */
  description: string;
  /** Iconify icon id. */
  icon: string;
  /**
   * Whether the backend can install this capability via
   * `system.install_capability`. Omit for capabilities that have no
   * install path (Docker, PackageKit itself).
   */
  installable?: {
    /** True when installing also requires PackageKit (i.e. there is a
     *  package step). The Install button is disabled when PackageKit is
     *  unavailable. False for pure service-start actions. */
    requiresPackageKit: boolean;
    /** True when the optional component requires Docker to be reachable. */
    requiresDocker?: boolean;
  };
  /** Display name in the capability manager. */
  label: string;
  /** Text shown when the capability is reachable. */
  readyText: string;
  /** Message when status is "unavailable". */
  reasonUnavailable: string;
  /** Message when status is "unknown". */
  reasonUnknown: string;
  /**
   * Optional destination surfaced as an action link ("Open …") on the
   * install-completion notification. Omit for capabilities with no dedicated
   * page (e.g. TuneD, Avahi).
   */
  route?: { href: string; label: string };
  /** camelCase field used in auth state (e.g. "dockerAvailable"). */
  state: string;
  /** snake_case prefix used on the wire (e.g. "docker" -> "docker_available", "docker_error"). */
  wire: string;
}

export const CAPABILITIES = [
  {
    wire: "docker",
    state: "dockerAvailable",
    label: "Docker",
    description: "Container dashboard and compose stack controls",
    readyText: "Docker is reachable.",
    dependency: "docker",
    icon: "mdi:docker",
    reasonUnknown: "Docker availability is still being checked.",
    reasonUnavailable: "Docker service is unavailable.",
  },
  {
    wire: "libvirt",
    state: "libvirtAvailable",
    label: "Virtual machines",
    description: "KVM/QEMU virtual machine management",
    readyText: "libvirt is reachable.",
    dependency: "libvirt",
    icon: "mdi:server",
    reasonUnknown: "libvirt availability is still being checked.",
    reasonUnavailable: "libvirt daemon is unavailable.",
    installable: { requiresPackageKit: true },
    route: { href: "/vm", label: "Open VMs" },
  },
  {
    wire: "watchtower",
    state: "watchtowerAvailable",
    label: "Watchtower",
    description: "Container auto-update engine",
    readyText: "LinuxIO Watchtower timer is installed.",
    dependency: "linuxio-watchtower",
    icon: "mdi:update",
    reasonUnknown: "Watchtower availability is still being checked.",
    reasonUnavailable: "LinuxIO Watchtower timer is not installed.",
    installable: { requiresPackageKit: false, requiresDocker: true },
  },
  {
    wire: "indexer",
    state: "indexerAvailable",
    label: "Indexer",
    description: "Search, folder sizes, and Docker stack indexing",
    readyText: "Indexer API is reachable.",
    dependency: "linuxio indexer",
    icon: "mdi:magnify-scan",
    reasonUnknown: "Indexer availability is still being checked.",
    reasonUnavailable: "Indexer API is unavailable.",
    installable: { requiresPackageKit: false },
  },
  {
    wire: "lm_sensors",
    state: "lmSensorsAvailable",
    label: "lm-sensors",
    description: "Hardware sensors and thermal readings",
    readyText: "sensors command is available.",
    dependency: "sensors",
    icon: "mdi:thermometer-lines",
    reasonUnknown: "lm-sensors dependency check is still running.",
    reasonUnavailable: "lm-sensors dependency is unavailable.",
    installable: { requiresPackageKit: true },
    route: { href: "/hardware", label: "Open hardware" },
  },
  {
    wire: "memory_inventory",
    state: "memoryInventoryAvailable",
    label: "Memory inventory",
    description: "Memory module slots, size, type, rank, and speed",
    readyText: "Memory module inventory is available.",
    dependency: "udev DMI data or dmidecode",
    icon: "mdi:memory",
    reasonUnknown: "Memory inventory availability is still being checked.",
    reasonUnavailable: "Memory module inventory is unavailable.",
    installable: { requiresPackageKit: true },
    route: { href: "/hardware", label: "Open hardware" },
  },
  {
    wire: "smartmontools",
    state: "smartmontoolsAvailable",
    label: "smartmontools",
    description: "Drive SMART attributes and self-tests",
    readyText: "smartctl command is available.",
    dependency: "smartctl",
    icon: "mdi:harddisk",
    reasonUnknown: "smartmontools dependency check is still running.",
    reasonUnavailable: "smartmontools dependency is unavailable.",
    installable: { requiresPackageKit: true },
    route: { href: "/storage", label: "Open storage" },
  },
  {
    wire: "packagekit",
    state: "packageKitAvailable",
    label: "PackageKit",
    description: "Package update checks and package operations",
    readyText: "PackageKit D-Bus service is reachable.",
    dependency: "PackageKit",
    icon: "mdi:package-variant-closed",
    reasonUnknown: "PackageKit availability is still being checked.",
    reasonUnavailable: "PackageKit D-Bus service is unavailable.",
  },
  {
    wire: "nfs_client",
    state: "nfsClientAvailable",
    label: "NFS client",
    description: "Mount external NFS exports",
    readyText: "NFS client utilities are available.",
    dependency: "nfs utilities",
    icon: "mdi:folder-network-outline",
    reasonUnknown: "NFS client utilities availability is still being checked.",
    reasonUnavailable: "NFS client utilities are unavailable.",
    installable: { requiresPackageKit: true },
    route: { href: "/shares", label: "Open shares" },
  },
  {
    wire: "nfs_server",
    state: "nfsServerAvailable",
    label: "NFS server",
    description: "Create and manage exported NFS shares",
    readyText: "NFS server utilities are available.",
    dependency: "exportfs",
    icon: "mdi:server-network",
    reasonUnknown: "NFS server utilities availability is still being checked.",
    reasonUnavailable: "NFS server utilities are unavailable.",
    installable: { requiresPackageKit: true },
    route: { href: "/shares", label: "Open shares" },
  },
  {
    wire: "samba_server",
    state: "sambaServerAvailable",
    label: "Samba (SMB)",
    description: "Create and manage SMB/CIFS file shares",
    readyText: "Samba server (smbd) is available.",
    dependency: "smbd",
    icon: "mdi:folder-network",
    reasonUnknown: "Samba server availability is still being checked.",
    reasonUnavailable: "Samba server is unavailable.",
    installable: { requiresPackageKit: true },
    route: { href: "/shares", label: "Open shares" },
  },
  {
    wire: "tuned",
    state: "tunedAvailable",
    label: "TuneD",
    description: "Power profile management",
    readyText: "TuneD D-Bus service is reachable.",
    dependency: "TuneD",
    icon: "mdi:lightning-bolt-outline",
    reasonUnknown: "TuneD availability is still being checked.",
    reasonUnavailable: "TuneD D-Bus service is unavailable.",
    installable: { requiresPackageKit: true },
  },
  {
    wire: "avahi",
    state: "avahiAvailable",
    label: "Avahi (mDNS)",
    description: "Reach this host at <hostname>.local from the LAN",
    readyText: "Avahi daemon is reachable.",
    dependency: "avahi-daemon",
    icon: "mdi:lan-connect",
    reasonUnknown: "Avahi availability is still being checked.",
    reasonUnavailable: "Avahi mDNS daemon is unavailable.",
    installable: { requiresPackageKit: true },
  },
  {
    wire: "wireguard",
    state: "wireguardAvailable",
    label: "WireGuard",
    description: "Create and manage WireGuard VPN interfaces",
    readyText: "wg-quick command is available.",
    dependency: "wg-quick",
    icon: "simple-icons:wireguard",
    reasonUnknown: "WireGuard tools availability is still being checked.",
    reasonUnavailable: "WireGuard tools are unavailable.",
    installable: { requiresPackageKit: true },
    route: { href: "/wireguard", label: "Open WireGuard" },
  },
] as const satisfies readonly CapabilityDef[];

// --- Derived types ------------------------------------------------------------

export type CapabilityWire = (typeof CAPABILITIES)[number]["wire"];
export type CapabilityKey = (typeof CAPABILITIES)[number]["state"];
export type CapabilitiesResponse = GeneratedCapabilitiesResponse;
export type CapabilityValueKey = Extract<
  keyof CapabilitiesResponse,
  `${CapabilityWire}_available`
>;
export type CapabilityErrorKey = Extract<
  keyof CapabilitiesResponse,
  `${CapabilityWire}_error`
>;

/** Frontend state shape: one tri-state field per capability (`null` = unknown). */
export type CapabilityState = Record<CapabilityKey, boolean | null>;

// --- Helpers ------------------------------------------------------------------

export const CAPABILITY_KEYS: readonly CapabilityKey[] = CAPABILITIES.map(
  (c) => c.state,
);

export const emptyCapabilityState: CapabilityState = Object.fromEntries(
  CAPABILITIES.map((c) => [c.state, null]),
) as CapabilityState;

/** Map a wire response (or partial) into frontend state, defaulting missing/non-boolean fields to null. */
export const capabilityStateFromWire = (
  wire: Partial<CapabilitiesResponse>,
): CapabilityState =>
  Object.fromEntries(
    CAPABILITIES.map((c) => {
      const v = wire[`${c.wire}_available` as CapabilityValueKey];
      return [c.state, typeof v === "boolean" ? v : null];
    }),
  ) as CapabilityState;

/** Pick only capability fields from a larger object, defaulting missing fields to null. */
export const pickCapabilityState = (
  src: Partial<CapabilityState>,
): CapabilityState =>
  Object.fromEntries(
    CAPABILITIES.map((c) => [c.state, src[c.state] ?? null]),
  ) as CapabilityState;

/** Parse capability state from untrusted JSON (e.g. localStorage). */
export const parseCapabilityState = (raw: unknown): CapabilityState => {
  if (!raw || typeof raw !== "object") return emptyCapabilityState;
  const obj = raw as Record<string, unknown>;
  return Object.fromEntries(
    CAPABILITIES.map((c) => {
      const v = obj[c.state];
      return [c.state, typeof v === "boolean" ? v : null];
    }),
  ) as CapabilityState;
};
