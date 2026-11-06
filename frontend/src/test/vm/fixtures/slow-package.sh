#!/bin/bash
# Run as root in the test VM. Installs version 1.0 of linuxio-e2e-slow and
# serves 2.0 from a local apt source, so PackageKit offers an update whose
# postinst takes 20 seconds.
set -euo pipefail

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
repo=/srv/linuxio-e2e-repo
for version in 1.0 2.0; do
	mkdir -p "${work}/${version}/DEBIAN"
	cat >"${work}/${version}/DEBIAN/control" <<CONTROL
Package: linuxio-e2e-slow
Version: ${version}
Architecture: all
Maintainer: LinuxIO e2e <e2e@localhost>
Description: LinuxIO e2e package whose upgrade takes 20 seconds
CONTROL
	# configure with a previous version is an upgrade.
	# shellcheck disable=SC2016 # expanded by the postinst, not here
	printf '#!/bin/sh\nif [ "$1" = configure ] && [ -n "$2" ]; then sleep 20; fi\n' >"${work}/${version}/DEBIAN/postinst"
	chmod 755 "${work}/${version}/DEBIAN/postinst"
	dpkg-deb --root-owner-group --build "${work}/${version}" "${work}/linuxio-e2e-slow_${version}.deb" >/dev/null
done

dpkg -i "${work}/linuxio-e2e-slow_1.0.deb" >/dev/null
rm -rf "$repo"
mkdir -p "$repo"
deb=linuxio-e2e-slow_2.0.deb
cp "${work}/${deb}" "${repo}/"
{
	dpkg-deb --field "${repo}/${deb}"
	echo "Filename: ./${deb}"
	echo "Size: $(stat -c %s "${repo}/${deb}")"
	echo "SHA256: $(sha256sum "${repo}/${deb}" | cut -d' ' -f1)"
} >"${repo}/Packages"
echo "deb [trusted=yes] file:${repo} ./" >/etc/apt/sources.list.d/linuxio-e2e.list
apt-get update -qq
