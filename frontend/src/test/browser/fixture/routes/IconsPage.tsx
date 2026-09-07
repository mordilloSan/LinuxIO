import { Icon } from "@iconify/react";

import { CAPABILITIES } from "@/api/capabilities";
import { distroIcons, getDistroIcon } from "@/icons/distro";

const icons = new Set([
  ...Object.values(distroIcons),
  getDistroIcon("unknown"),
  CAPABILITIES.find((item) => item.wire === "wireguard")!.icon,
]);

export default function IconsPage() {
  return (
    <div>
      {[...icons].map((icon) => (
        <Icon
          aria-hidden={false}
          aria-label={icon}
          height={32}
          icon={icon}
          key={icon}
          role="img"
          width={32}
        />
      ))}
    </div>
  );
}
