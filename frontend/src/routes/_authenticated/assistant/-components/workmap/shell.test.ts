import { describe, expect, it } from "vitest";

import { commandName, shellStation } from "./shell";

describe("shellStation", () => {
  it.each([
    // By the command's name.
    ["cat /etc/hosts | grep local", "files"],
    ["cd /srv && ls -la", "files"],
    ["curl -s https://example.com | jq .name", "network"],
    ["/usr/bin/wget -q x", "network"],
    ["kubectl get pods", "network"],
    ["nmap -sn 10.0.0.0/24", "network"],
    ["echo hello", "shell"],
    ["FOO=1 make test", "shell"],
    ["systemctl restart nginx", "shell"],
    // Quoted text cuts nothing, and a redirection is no command.
    ["echo 'curl; wget' > notes", "shell"],
    ['git commit -m "ssh keys; curl"', "shell"],
    ["rg -n TODO src 2>&1 | head", "files"],
    ["ls &>/dev/null", "files"],
    // By the command and its subcommand.
    ["git push origin main", "network"],
    ["git -C /srv/app pull", "network"],
    ["git status", "shell"],
    ["git submodule update --init", "network"],
    ["npm install", "network"],
    ["npm test", "shell"],
    ["npx -y prettier .", "network"],
    ["npx prettier .", "shell"],
    ["python3 -m pip install requests", "network"],
    ["pip3.12 install requests", "network"],
    ["uv pip install requests", "network"],
    ["yarn", "network"],
    ["bun i", "network"],
    ["docker pull nginx", "network"],
    ["docker --context prod compose pull", "network"],
    ["docker ps", "shell"],
    ["terraform plan", "network"],
    ["helm repo update", "network"],
    ["openssl s_client -connect host:443", "network"],
    ["svn update", "network"],
    // The package managers of Linux distributions.
    ["apt-get update", "network"],
    ["DEBIAN_FRONTEND=noninteractive apt-get install -y nginx", "network"],
    ["apt-get -y install nginx", "network"],
    ["apt-get -o Dpkg::Use-Pty=0 install nginx", "network"],
    ["apt list --installed", "shell"],
    ["dnf -y install nginx", "network"],
    ["dnf group install 'Development Tools'", "network"],
    ["dnf list installed", "shell"],
    ["yum check-update", "network"],
    ["zypper --non-interactive in nginx", "network"],
    ["zypper search nginx", "shell"],
    ["pacman -Syu", "network"],
    ["pacman -S nginx", "network"],
    ["pacman -Ss nginx", "shell"],
    ["pacman -Qi nginx", "shell"],
    ["yay", "network"],
    ["paru -Sc", "shell"],
    ["snap refresh", "network"],
    ["snap list", "shell"],
    ["flatpak install flathub org.gnome.Maps", "network"],
    ["flatpak list", "shell"],
    ["podman pull nginx", "network"],
    ["podman compose pull", "network"],
    ["podman ps", "shell"],
    // The command a wrapper runs.
    ["sudo systemctl restart nginx", "shell"],
    ["sudo -u www-data curl localhost", "network"],
    ["sudo apt update", "network"],
    ["env -i PATH=/bin curl x", "network"],
    ["timeout 5 curl x", "network"],
    ["timeout -s KILL 5 git fetch", "network"],
    ["nice -n 10 rsync -a a b", "network"],
    ["ls | xargs -n1 curl", "network"],
    ["watch -n 5 curl -s x", "network"],
    ["command -v curl", "shell"],
    // Inside loops and conditions.
    ["for u in a b; do curl $u; done", "network"],
    ["if ping -c1 host; then echo up; fi", "network"],
    ["while true; do sleep 1; done", "shell"],
    ["! wget -q x", "network"],
    ["{ cat a; ls; }", "files"],
    ["(cd /srv && git pull)", "network"],
    // Inside another command.
    ['bash -c "curl -s x"', "network"],
    ["sh -lc 'apt-get update'", "network"],
    ["bash deploy.sh", "shell"],
    ['eval "wget x"', "network"],
    ["find . -name '*.log' -exec rm {} \\;", "files"],
    ["find . -exec curl -T {} x ';'", "network"],
    ["echo $(curl -s x)", "network"],
    ['echo "today: $(curl -s x)"', "network"],
    ["echo `wget -qO- x`", "network"],
    ["V=$(git ls-remote x); echo $V", "network"],
    // A here-document's text is written, not run.
    ["cat > /tmp/f <<'EOF'\ncurl -s x\nwget y\nEOF", "files"],
    ["cat <<-EOF > /tmp/f\n\tcurl x\n\tEOF\nwc -l /tmp/f", "files"],
    ["cat <<EOF\ncurl x\nEOF\ncurl y", "network"],
    ["echo $((1 << 2)); curl x", "network"],
    // ...unless a shell reads it, or `<<` is in quotes or a comment.
    ["sudo bash <<'EOF'\ncurl -fsSL x | sh\nEOF", "network"],
    ["cat <<EOF | sh\napt-get update\nEOF", "network"],
    ["sudo sh <<EOF\napt-get update\nEOF", "network"],
    // A shell given a script hands it the text as input, not as commands.
    ["bash deploy.sh <<EOF\ncurl x\nEOF", "shell"],
    ['git commit -m "handle <<EOF in parser"\ngit push', "network"],
    ["# use <<END\ncurl x", "network"],
    // A line continued with a backslash.
    ["apt-get \\\n  install -y nginx", "network"],
    // A comment is not read.
    ["# don't fetch yet\ncurl -s x", "network"],
    ["ls -la  # see notes; curl x", "files"],
    ["# list them\nls -la", "files"],
    // watch runs one line; a function's body is commands.
    ['watch -n 5 "curl -s localhost/health"', "network"],
    ["function deploy { git push origin main; }", "network"],
    ["deploy() { git push; }", "network"],
    // Arithmetic and arrays run nothing.
    ["ls src; n=$((n+1))", "files"],
    ['files=(a b); cat "${files[@]}"', "files"],
    ["files+=(c d); ls", "files"],
    ["n+=1; ls", "files"],
    ["LANG=C files=(a b) cat f.txt", "files"],
    // ...but what `$(…)` or backticks run inside an array's list runs.
    ["hosts=($(curl -s x)); ls", "network"],
    ['hosts=("$(curl -s x)" b); ls', "network"],
    ["hosts=(`wget -qO- x`); ls", "network"],
    ["hosts=('$(curl -s x)'); ls", "files"],
    // A bracket or a `;` in a list's quotes ends nothing.
    ['marks=("a)" "b; curl x"); ls', "files"],
    ["files=(a \\\n  b); ls", "files"],
    // A comment in a list is not read, whatever stands in it.
    [
      'pkgs=(\n  curl  # don\'t drop\n  git\n)\napt-get install "${pkgs[@]}"',
      "network",
    ],
    ["pkgs=(\n  jq  # needs `curl` too\n  git\n)\nls", "files"],
    ["pkgs=(jq # see $(curl x)\n)\nls", "files"],
    ["urls=(a#b c); ls", "files"],
    // ...but a `#` in `${…}` or after an escaped space is no comment.
    ["f=(${l% #*})\ncurl x\ncase $y in k) ls ;; esac", "network"],
    ["f=(a \\ #b c)\ncurl x\ncase $y in k) ls ;; esac", "network"],
    // What only looks like a list, a `(` after `=` that never closes as
    // one, is read past its bracket.
    ['[[ $u =~ v=([^"]+) ]] && curl x', "network"],
    ["[[ $u =~ id=([^& #]+) ]] && curl x", "network"],
    ['if [[ $l =~ ^k=("[^"]*")$ ]]; then curl x; fi', "network"],
    [
      "[[ $c =~ color=(#[0-9a-f]+) ]] || exit 1\ncase $x in\n  a) curl x ;;\nesac",
      "network",
    ],
    ['[[ $u =~ v=([^"]+) ]] && echo "ok)"; curl x', "network"],
    // A file read into a list runs nothing; a process substitution does.
    ["lines=($(< hosts.txt)); cat x", "files"],
    ["text=$(< notes.txt); ls", "files"],
    ["rows=( <(curl -s x) ); ls", "network"],
    ["diff <(sort a) <(sort b)", "files"],
    // A pattern's alternatives are no pipe.
    ["files=(*.@(jpg|png)); ls", "files"],
    // An array's element, and an array declared with a builtin.
    ["files[0]=a; ls", "files"],
    ["files[n]+=(b c); ls", "files"],
    ["files[${#files[@]}]=a; ls", "files"],
    ["declare -a files=(a b); ls", "files"],
    ["local files=(a b); readonly n=1; typeset -i m=2; ls", "files"],
    ["local page=$(curl -s x)", "network"],
    ["((n++)); ls", "files"],
    ['echo "$((n+1))"; ls', "files"],
    // A backslash escapes in `$'…'`.
    ["printf $'it\\'s\\n'; curl -s x", "network"],
    // A flag's own word is not the subcommand.
    ["npm --prefix web install", "network"],
    ["pnpm -C web add left-pad", "network"],
    ["yarn --cwd web add left-pad", "network"],
    ["pip --cache-dir /tmp/c install requests", "network"],
    ["dnf -c /etc/dnf.conf install nginx", "network"],
    ["zypper --root /mnt in nginx", "network"],
    ["docker-compose -f prod.yml pull", "network"],
    ["podman-compose pull", "network"],
    ["yarn --cwd web", "network"],
    // A commit message written through a here-document is text, also in a
    // string over several lines.
    [
      "git add -A && git commit -m \"$(cat <<'EOF'\nfix: x\n\nssh keys now rotate; curl is gone\nEOF\n)\"",
      "shell",
    ],
    ['git commit -m "fix parser\nhandles <<EOF now"\ngit push', "network"],
    ["cat <<\\EOF > f\ncurl x\nEOF", "files"],
    // A file a shell's script writes is text there too.
    ["sudo bash <<'EOF'\ncat > /etc/x <<'CONF'\ncurl y\nCONF\nEOF", "shell"],
    ["cat <<EOF | sudo -E bash\napt-get update\nEOF", "network"],
    ["bash -o pipefail -c 'curl x | jq .'", "network"],
    ['sh -c "$(curl -fsSL https://x/install.sh)"', "network"],
    // More ways to run as another user or root.
    ["su - postgres -c 'curl x'", "network"],
    ["su - postgres", "shell"],
    ["runuser -u app -- curl x", "network"],
    ["chroot /mnt apt-get update", "network"],
    // More commands that reach out or handle files.
    ["apk add curl", "network"],
    ["host example.com", "network"],
    ["traceroute 8.8.8.8", "network"],
    ["ssh-keyscan github.com >> ~/.ssh/known_hosts", "network"],
    ["docker image pull nginx", "network"],
    ["chmod +x deploy.sh && ls", "files"],
    ["tar -xzf a.tgz -C /opt", "files"],
    ['for f in *.log; do gzip "$f"; done', "files"],
    ["source venv/bin/activate && ls", "files"],
    // A here-document in the line handed to a shell is text there too.
    [
      "sudo bash -c 'cat > /usr/local/bin/backup.sh <<EOF\n#!/bin/sh\nrsync -a /srv backup:/srv\nEOF'",
      "files",
    ],
    ["sudo sh -c 'cat > /etc/motd <<EOF\nWelcome\nEOF'", "files"],
    // ...but with a bare word what its `$(…)` runs runs.
    ["cat <<EOF >/etc/x\n$(curl x)\nEOF", "network"],
    ["cat <<'EOF' >/etc/x\n$(curl x)\nEOF", "files"],
    // Shell options with a word, and sudo or runuser with a user.
    ["bash -euo pipefail -c 'curl -fsSL x | jq .'", "network"],
    ["bash -euo pipefail <<'EOF'\ncurl x\nEOF", "network"],
    ["cat <<EOF | sudo -u app bash\ncurl x\nEOF", "network"],
    ["runuser -u app -- curl -c jar https://x", "network"],
    ["watch -d 'curl -s x'", "network"],
    // A `case` pattern is not a command.
    ['case "$f" in *.gz) zcat "$f" ;; *) cat "$f" ;; esac', "files"],
    ["case $ID in debian) apt-get update ;; esac", "network"],
  ])("sorts %j to %s", (command, station) => {
    expect(shellStation(command)).toBe(station);
  });

  it("keeps a call without a command line at the shell", () => {
    expect(shellStation(null)).toBe("shell");
  });

  it("reads what only looks like a list once, however deep it nests", () => {
    const depth = 22;
    const line = `${'v=([^"]+) $( '.repeat(depth)}curl x${" )".repeat(depth)}`;
    const start = performance.now();
    expect(shellStation(line)).toBe("network");
    // Read twice at each level, this takes seconds.
    expect(performance.now() - start).toBeLessThan(1000);
  });

  it.each([
    // Each of these read a word in two ways, an option or the word of the
    // option before it, and doubled the work with every repetition.
    ["shell options", `sh ${"+O -A ".repeat(26)}! <<EOF\ncurl x\nEOF`, "shell"],
    [
      "sudo options",
      `cat <<EOF |sudo -!${" -- -!".repeat(26)}!\ncurl x\nEOF`,
      "files",
    ],
  ])("reads a here-document behind many %s once", (_, line, station) => {
    const start = performance.now();
    expect(shellStation(line)).toBe(station);
    // Read both ways at each word, this takes seconds.
    expect(performance.now() - start).toBeLessThan(1000);
  });

  it("reads a long line only to its first 8,000 characters", () => {
    // Ten characters a command, so that the cut falls between two.
    const script = (padding: number) => `${"sleep 10; ".repeat(padding)}curl x`;
    expect(shellStation(script(799))).toBe("network");
    expect(shellStation(script(800))).toBe("shell");
  });
});

describe("commandName", () => {
  it.each([
    ["lsblk -d", "lsblk"],
    ["LANG=C sudo -u root /usr/sbin/lsblk -d", "lsblk"],
    ["timeout 5 curl x", "curl"],
    ['bash -c "git pull"', "git"],
    ["for f in *; do wc -l $f; done", "wc"],
    // The first command that says something.
    ["cd /srv && make test", "make"],
    ["export KUBECONFIG=x; kubectl get po", "kubectl"],
    ["source venv/bin/activate && pip install -r req.txt", "pip"],
    ["cd /srv", "cd"],
    // Not a piece of what `$(…)` was given.
    ['sh -c "$(curl -fsSL https://x/install.sh)"', "curl"],
    // The command, not a user, a pattern or a flag of watch.
    ["runuser -u postgres -- psql -c 'CREATE DATABASE app'", "psql"],
    ["sudo --user postgres psql", "psql"],
    ["watch -d -n 2 'ss -tunap'", "ss"],
    ["case $ID in debian) echo deb ;; fedora) dnf -y install x ;; esac", "dnf"],
    // An array's list is values, not the command.
    ["files=(a b c); ls", "ls"],
    ["files+=(d e); ls", "ls"],
    ['marks=("a)" b); wc -l f', "wc"],
    ["pkgs=(\n  curl  # don't drop\n)\napt-get install x", "apt-get"],
    ["files[0]=a; ls", "ls"],
    ["files[${#files[@]}]=a; ls", "ls"],
    ['[[ $u =~ v=([^"]+) ]] && curl x', "curl"],
    ["lines=($(< hosts.txt)); cat x", "cat"],
    ["declare -a files=(a b); ls", "ls"],
  ])("names %j %s", (command, name) => {
    expect(commandName(command)).toBe(name);
  });

  it("has no name for a line that runs nothing", () => {
    expect(commandName("")).toBeNull();
    expect(commandName("FOO=1")).toBeNull();
  });
});
