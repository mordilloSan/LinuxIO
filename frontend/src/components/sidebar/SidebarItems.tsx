import { Icon } from "@iconify/react";
import Cpu from "lucide-react/dist/esm/icons/cpu";
import Folder from "lucide-react/dist/esm/icons/folder";
import HardDrive from "lucide-react/dist/esm/icons/hard-drive";
import Home from "lucide-react/dist/esm/icons/home";
import Network from "lucide-react/dist/esm/icons/network";
import RefreshCcw from "lucide-react/dist/esm/icons/refresh-ccw";
import ServerCog from "lucide-react/dist/esm/icons/server-cog";
import Share2 from "lucide-react/dist/esm/icons/share-2";
import Terminal from "lucide-react/dist/esm/icons/terminal";
import Users from "lucide-react/dist/esm/icons/users";

import { SidebarItemsType } from "@/types/sidebar";

const SidebarItems: SidebarItemsType[] = [
  {
    href: "/",
    icon: Home,
    title: "Dashboard",
  },
  {
    href: "/network",
    icon: Network,
    title: "Network",
  },
  {
    href: "/updates",
    icon: RefreshCcw,
    title: "Updates",
  },
  {
    href: "/services",
    icon: ServerCog,
    title: "Services",
  },
  {
    href: "/storage",
    icon: HardDrive,
    title: "Storage",
  },
  {
    href: "/docker",
    icon: () => <Icon icon="fa-brands:docker" />,
    title: "Docker",
  },
  {
    href: "/accounts",
    icon: Users,
    title: "Accounts",
  },
  {
    href: "/shares",
    icon: Share2,
    title: "Shares",
  },
  {
    href: "/wireguard",
    icon: () => <Icon icon="cib:wireguard" width="48" height="48" />,
    title: "Wireguard",
  },
  {
    href: "/hardware",
    icon: Cpu,
    title: "Hardware",
  },
  {
    href: "/filebrowser",
    icon: Folder,
    title: "Navigator",
  },
  {
    href: "/terminal",
    icon: Terminal,
    title: "Terminal",
  },
];

export default SidebarItems;
