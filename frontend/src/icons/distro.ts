// Literal IDs let the existing icon generator bundle only these distro logos.
export const distroIcons: Record<string, string> = {
  almalinux: "simple-icons:almalinux",
  alpine: "simple-icons:alpinelinux",
  arch: "simple-icons:archlinux",
  archarm: "simple-icons:archlinux",
  centos: "simple-icons:centos",
  debian: "simple-icons:debian",
  elementary: "simple-icons:elementary",
  fedora: "simple-icons:fedora",
  gentoo: "simple-icons:gentoo",
  kali: "simple-icons:kalilinux",
  linuxmint: "simple-icons:linuxmint",
  manjaro: "simple-icons:manjaro",
  nixos: "simple-icons:nixos",
  opensuse: "simple-icons:opensuse",
  "opensuse-leap": "simple-icons:opensuse",
  "opensuse-tumbleweed": "simple-icons:opensuse",
  pop: "simple-icons:popos",
  raspbian: "simple-icons:raspberrypi",
  rhel: "simple-icons:redhat",
  redhat: "simple-icons:redhat",
  rocky: "simple-icons:rockylinux",
  sles: "simple-icons:suse",
  sled: "simple-icons:suse",
  suse: "simple-icons:suse",
  slackware: "simple-icons:slackware",
  ubuntu: "simple-icons:ubuntu",
  void: "simple-icons:voidlinux",
  zorin: "simple-icons:zorin",
};

export function getDistroIcon(platform: string): string {
  return Object.hasOwn(distroIcons, platform)
    ? distroIcons[platform]
    : "simple-icons:linux";
}
