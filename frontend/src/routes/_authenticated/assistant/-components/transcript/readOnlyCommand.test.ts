import { describe, expect, it } from "vitest";

import { isReadOnlyCommand } from "./readOnlyCommand";

describe("isReadOnlyCommand", () => {
  it.each([
    "ls -la /etc",
    "systemctl status nginx",
    "sudo systemctl --no-pager status nginx",
    "journalctl -u nginx -b | tail -n 50",
    "ss -ltnp 'sport = :80'",
    "cat /etc/os-release 2>/dev/null",
    "df -h && free -m",
    "docker compose ps",
    "btrfs filesystem usage /",
    "ip addr show",
    "LANG=C date",
    "find /var -name '*.log'",
    "timedatectl",
    "rg -n --no-heading -i '^\\s*(PubkeyAuthentication|PasswordAuthentication)' /etc/ssh",
    'grep -E "a|b; c > d" /etc/hosts',
    "echo 'a > b' | wc -c",
  ])("treats %s as reading", (command) => {
    expect(isReadOnlyCommand(command)).toBe(true);
  });

  it.each([
    "systemctl restart nginx",
    "systemctl disable --now apache2",
    "rm -rf /tmp/x",
    "echo hi > /etc/motd",
    "cat $(which nginx)",
    "ls; reboot",
    "sed -i s/a/b/ /etc/hosts",
    "find /var -name '*.log' -delete",
    "journalctl --vacuum-time=2d",
    "ip link set eth0 down",
    "docker rm web",
    "apt upgrade",
    "git checkout main",
    "timedatectl set-timezone UTC",
    'echo "$(reboot)"',
    "echo 'x' > /etc/motd",
    "find / '-delete'",
  ])("treats %s as acting", (command) => {
    expect(isReadOnlyCommand(command)).toBe(false);
  });
});
