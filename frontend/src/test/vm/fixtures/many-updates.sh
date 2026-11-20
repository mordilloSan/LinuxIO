#!/bin/bash
# Run as root in the test VM. Installs version 1.0 of 150 packages named
# linuxio-e2e-many-NNN and serves 2.0 of each from a local apt source, so
# PackageKit answers the update list with a burst of signals far longer than
# the list's signal buffer. "remove" as the first argument undoes it.
set -euo pipefail

repo=/srv/linuxio-e2e-many-repo
list=/etc/apt/sources.list.d/linuxio-e2e-many.list
count=150

if [[ "${1:-}" == remove ]]; then
	mapfile -t installed < <(dpkg-query -W -f '${Package}\n' 'linuxio-e2e-many-*' 2>/dev/null || true)
	if ((${#installed[@]} > 0)); then
		dpkg --purge "${installed[@]}" >/dev/null
	fi
	rm -rf "$repo" "$list"
	apt-get update -qq
	exit 0
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
rm -rf "$repo"
mkdir -p "$repo"
for n in $(seq -w 1 "$count"); do
	name="linuxio-e2e-many-${n}"
	for version in 1.0 2.0; do
		dir="${work}/${name}_${version}"
		mkdir -p "${dir}/DEBIAN"
		cat >"${dir}/DEBIAN/control" <<CONTROL
Package: ${name}
Version: ${version}
Architecture: all
Maintainer: LinuxIO e2e <e2e@localhost>
Description: LinuxIO e2e package ${n} of a long update list
CONTROL
		dpkg-deb --root-owner-group --build "$dir" "${work}/${name}_${version}.deb" >/dev/null
	done
	deb="${name}_2.0.deb"
	cp "${work}/${deb}" "${repo}/"
	{
		dpkg-deb --field "${repo}/${deb}"
		echo "Filename: ./${deb}"
		echo "Size: $(stat -c %s "${repo}/${deb}")"
		echo "SHA256: $(sha256sum "${repo}/${deb}" | cut -d' ' -f1)"
		echo
	} >>"${repo}/Packages"
done

dpkg -i "${work}"/linuxio-e2e-many-*_1.0.deb >/dev/null
echo "deb [trusted=yes] file:${repo} ./" >"$list"
apt-get update -qq
