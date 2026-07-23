import type { ElementType } from "react";

import type { AccessPolicy, CapabilityKey } from "@/hooks/useCapabilities";
import {
  CpuIcon,
  DockerIcon,
  FileTextIcon,
  FolderIcon,
  HardDriveIcon,
  HomeIcon,
  NetworkIcon,
  RefreshCcwIcon,
  ServerCogIcon,
  ShareIcon,
  TerminalIcon,
  UsersIcon,
  VirtualMachineIcon,
  WireguardIcon,
} from "@/icons/svg";

export type ProtectedRouteId =
  | "dashboard"
  | "network"
  | "updates"
  | "services"
  | "logs"
  | "storage"
  | "docker"
  | "vm"
  | "accounts"
  | "shares"
  | "wireguard"
  | "hardware"
  | "filebrowser"
  | "terminal";

export interface ProtectedRouteCatalogEntry extends AccessPolicy {
  id: ProtectedRouteId;
  /** Path relative to the authenticated application layout. */
  path:
    | ""
    | "network"
    | "updates"
    | "services"
    | "logs"
    | "storage"
    | "docker"
    | "vm"
    | "accounts"
    | "shares"
    | "wireguard"
    | "hardware"
    | "filebrowser/$"
    | "terminal";
  sidebar: {
    href?: string;
    title: string;
    icon: ElementType | string;
    position: number;
  };
}

function capabilities(
  ...requiredCapabilities: CapabilityKey[]
): Pick<ProtectedRouteCatalogEntry, "requiredCapabilities"> {
  return { requiredCapabilities };
}

/**
 * Router-independent protected-route metadata.
 *
 * The router and sidebar share this path, access, and presentation metadata.
 * Executable components and loaders stay outside the catalog.
 */
export const protectedRouteCatalog = [
  {
    id: "dashboard",
    path: "",
    sidebar: {
      title: "Dashboard",
      icon: HomeIcon,
      position: 0,
    },
  },
  {
    id: "network",
    path: "network",
    sidebar: {
      title: "Network",
      icon: NetworkIcon,
      position: 10,
    },
  },
  {
    id: "updates",
    path: "updates",
    sidebar: {
      title: "Updates",
      icon: RefreshCcwIcon,
      position: 20,
    },
  },
  {
    id: "services",
    path: "services",
    sidebar: {
      title: "Services",
      icon: ServerCogIcon,
      position: 30,
    },
  },
  {
    id: "logs",
    path: "logs",
    sidebar: {
      title: "Logs",
      icon: FileTextIcon,
      position: 35,
    },
  },
  {
    id: "storage",
    path: "storage",
    sidebar: {
      title: "Storage",
      icon: HardDriveIcon,
      position: 40,
    },
  },
  {
    id: "docker",
    path: "docker",
    ...capabilities("dockerAvailable"),
    sidebar: {
      title: "Docker",
      icon: DockerIcon,
      position: 50,
    },
  },
  {
    id: "vm",
    path: "vm",
    requiresPrivileged: true,
    ...capabilities("libvirtAvailable"),
    sidebar: {
      title: "VMs",
      icon: VirtualMachineIcon,
      position: 55,
    },
  },
  {
    id: "accounts",
    path: "accounts",
    sidebar: {
      title: "Accounts",
      icon: UsersIcon,
      position: 60,
    },
  },
  {
    id: "shares",
    path: "shares",
    sidebar: {
      title: "Shares",
      icon: ShareIcon,
      position: 70,
    },
  },
  {
    id: "wireguard",
    path: "wireguard",
    requiresPrivileged: true,
    ...capabilities("wireguardAvailable"),
    sidebar: {
      title: "Wireguard",
      icon: WireguardIcon,
      position: 80,
    },
  },
  {
    id: "hardware",
    path: "hardware",
    ...capabilities("lmSensorsAvailable"),
    sidebar: {
      title: "Hardware",
      icon: CpuIcon,
      position: 90,
    },
  },
  {
    id: "filebrowser",
    path: "filebrowser/$",
    sidebar: {
      title: "Navigator",
      icon: FolderIcon,
      position: 100,
      href: "/filebrowser",
    },
  },
  {
    id: "terminal",
    path: "terminal",
    sidebar: {
      title: "Terminal",
      icon: TerminalIcon,
      position: 110,
    },
  },
] as const satisfies readonly ProtectedRouteCatalogEntry[];
