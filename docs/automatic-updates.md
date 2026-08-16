# Automatic Updates

LinuxIO configures each supported distribution's native automatic-update
provider. PackageKit remains responsible for discovering updates and applying
offline updates; it does not own the recurring automatic-update schedule.

## Supported Providers

| Distribution | Provider | Managed systemd timer |
|---|---|---|
| Ubuntu | `unattended-upgrades` | `apt-daily.timer`, `apt-daily-upgrade.timer` |
| Debian | `unattended-upgrades` | `apt-daily.timer`, `apt-daily-upgrade.timer` |
| Linux Mint | Mint Update Manager automation | `mintupdate-automation-upgrade.timer` |
| RHEL, Rocky Linux, AlmaLinux (DNF4) | `dnf-automatic` | `dnf-automatic.timer` |
| Fedora (DNF5) | DNF5 automatic plugin | `dnf5-automatic.timer` |

Backend selection uses `/etc/os-release` and checks the distribution ID before
`ID_LIKE`. This ensures Linux Mint uses its own Update Manager instead of being
treated as Ubuntu merely because it belongs to the Ubuntu and Debian families.
When DNF4 and DNF5 compatibility surfaces coexist, LinuxIO prefers the native
DNF5 provider.

## Supported Settings

The server reports the settings supported by the selected provider. The UI
only offers those values, and the server validates them again before changing
configuration.

| Provider | Frequency | Scope | Download only | Reboot policy | Exclusions |
|---|---|---|---|---|---|
| Ubuntu/Debian APT | Hourly, daily, weekly | Security, updates, all | Yes | Never, if needed | Yes |
| Linux Mint | Hourly, daily, weekly | Security, all enabled repositories | No | Never | Yes |
| DNF4 | Hourly, daily, weekly | Security, all enabled repositories | Yes | Never; if needed/always when supported by the installed version | Yes |
| DNF5 | Hourly, daily, weekly | Security, all enabled repositories | Yes | Never, if needed, always | Yes |

An unavailable provider is reported as not configurable with an installation
note. LinuxIO leaves its controls read-only until the native package or plugin
is installed.

## Configuration Ownership

LinuxIO uses explicit configuration files and systemd timer drop-ins where the
provider supports them. It never replaces distribution-owned defaults
wholesale. Provider-specific settings remain separate because their formats
and semantics differ:

- Ubuntu and Debian use distribution-specific unattended-upgrades origin
  rules, plus LinuxIO-owned APT and systemd drop-ins.
- Linux Mint uses `mintupdate-automation` and its blacklist rather than
  installing or configuring a second unattended-upgrades scheduler.
- DNF4 and DNF5 use separate implementations. LinuxIO updates only the managed
  keys in `/etc/dnf/automatic.conf`, preserving administrator-owned sections,
  comments, emitters, and network settings.

Disabling LinuxIO-managed automatic installation does not require disabling
`apt-daily.timer`. APT may continue using that timer to refresh package metadata;
`apt-daily-upgrade.timer` is the unit that performs unattended installation.
