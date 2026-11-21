#!/usr/bin/env bash
# Publish the LinuxIO DEBs of a folder into a signed static APT repository
# (docs/TODO/packaging.md, "Publish signed static repositories"):
#
#   OUT_DIR/linuxio.sources                     deb822, the public key in Signed-By
#   OUT_DIR/pool/main/l/linuxio/linuxio_<version>_amd64.deb
#   OUT_DIR/dists/stable/{InRelease,Release,Release.gpg}
#   OUT_DIR/dists/stable/main/binary-amd64/Packages{,.gz}
#   OUT_DIR/dists/stable/main/binary-amd64/by-hash/SHA256/<sum>
#
# A package already in OUT_DIR stays, so publishing one more version keeps
# the earlier ones; the metadata is generated again over all of them with
# apt-ftparchive. A version already published with other bytes is refused:
# clients that read the first one would see a hash mismatch.
#
# KEY names a key already in the caller's GnuPG keyring (GNUPGHOME): a
# passphrase-less signing subkey in CI, a throwaway key in a test VM. This
# script never creates, imports or prints a secret key; it exports only the
# public key, into linuxio.sources. BASE_URL is the address linuxio.sources
# points apt at, the URL of OUT_DIR once it is published (https://.../apt, or
# file:///... in a test VM). The repository is never marked trusted=yes.
#
# Usage: repo-apt.sh --packages-dir DIR --key KEYID --base-url URL --out-dir DIR
set -euo pipefail
umask 022

die() {
	echo "repo-apt: $*" >&2
	exit 1
}

usage="usage: repo-apt.sh --packages-dir DIR --key KEYID --base-url URL --out-dir DIR"
packages_dir=""
key=""
base_url=""
out_dir=""
while [[ $# -gt 0 ]]; do
	case "$1" in
	--packages-dir | --key | --base-url | --out-dir)
		[[ $# -ge 2 && -n $2 ]] || die "$1 needs a value"
		case "$1" in
		--packages-dir) packages_dir="$2" ;;
		--key) key="$2" ;;
		--base-url) base_url="$2" ;;
		--out-dir) out_dir="$2" ;;
		esac
		shift 2
		;;
	*) die "unknown argument ${1} (${usage})" ;;
	esac
done
[[ -n $packages_dir && -n $key && -n $base_url && -n $out_dir ]] || die "$usage"
[[ $base_url =~ ^(https|http|file):// ]] || die "--base-url '${base_url}' is not an https://, http:// or file:// URL"
base_url="${base_url%/}"
[[ -d $packages_dir ]] || die "packages folder ${packages_dir} does not exist"

command -v apt-ftparchive >/dev/null || die "apt-ftparchive is missing: install apt-utils"
command -v gpg >/dev/null || die "gpg is missing: install gnupg"
command -v dpkg-deb >/dev/null || die "dpkg-deb is missing: install dpkg"
gpg --batch --list-secret-keys -- "$key" >/dev/null 2>&1 ||
	die "no secret key '${key}' in the GnuPG keyring${GNUPGHOME:+ ${GNUPGHOME}}"

shopt -s nullglob
debs=("${packages_dir}"/linuxio_*_amd64.deb)
[[ ${#debs[@]} -gt 0 ]] || die "no linuxio_*_amd64.deb in ${packages_dir}"

mkdir -p "$out_dir"
out_dir="$(realpath -- "$out_dir")"
pool="${out_dir}/pool/main/l/linuxio"
dist="${out_dir}/dists/stable"
index="${dist}/main/binary-amd64"
mkdir -p "$pool" "$index"

# 1. The packages, named as the pool names them.
for deb in "${debs[@]}"; do
	name="$(dpkg-deb --field "$deb" Package)" || die "cannot read ${deb}"
	version="$(dpkg-deb --field "$deb" Version)"
	arch="$(dpkg-deb --field "$deb" Architecture)"
	[[ $name == linuxio && $arch == amd64 ]] || die "${deb} is ${name} ${arch}, not linuxio amd64"
	dest="${pool}/linuxio_${version}_amd64.deb"
	if [[ -e $dest ]]; then
		cmp -s "$deb" "$dest" || die "linuxio ${version} is already published with other contents (${dest})"
		echo "repo-apt: linuxio ${version} is already published" >&2
		continue
	fi
	cp -- "$deb" "${dest}.part"
	mv -f -- "${dest}.part" "$dest"
	echo "repo-apt: added linuxio ${version}" >&2
done

# 2. The index, with SHA-256 sums only: apt fetches an index by the strongest
# hash Release lists, and the by-hash copies below are SHA256.
hash_options=(-o APT::FTPArchive::MD5=false -o APT::FTPArchive::SHA1=false
	-o APT::FTPArchive::SHA256=true -o APT::FTPArchive::SHA512=false)
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
(cd "$out_dir" && apt-ftparchive "${hash_options[@]}" packages pool/main) >"${work}/Packages"
[[ -s "${work}/Packages" ]] || die "apt-ftparchive listed no package"
gzip -9n <"${work}/Packages" >"${work}/Packages.gz"
mkdir -p "${index}/by-hash/SHA256"
for file in Packages Packages.gz; do
	sum="$(sha256sum <"${work}/${file}" | cut -d' ' -f1)"
	cp -- "${work}/${file}" "${index}/by-hash/SHA256/${sum}"
	mv -f -- "${work}/${file}" "${index}/${file}"
done

# 3. Release over the index, then its two signatures. The earlier Release and
# its signatures go first, so the new Release does not list them.
rm -f "${dist}/Release" "${dist}/InRelease" "${dist}/Release.gpg"
(cd "$dist" && apt-ftparchive "${hash_options[@]}" \
	-o APT::FTPArchive::Release::Origin=LinuxIO \
	-o APT::FTPArchive::Release::Label=LinuxIO \
	-o APT::FTPArchive::Release::Suite=stable \
	-o APT::FTPArchive::Release::Codename=stable \
	-o APT::FTPArchive::Release::Architectures=amd64 \
	-o APT::FTPArchive::Release::Components=main \
	-o APT::FTPArchive::Release::Acquire-By-Hash=yes \
	-o APT::FTPArchive::Release::Description="LinuxIO packages" \
	release .) >"${work}/Release"
grep -q '^Acquire-By-Hash: yes$' "${work}/Release" || die "apt-ftparchive wrote no Acquire-By-Hash field"
grep -q ' main/binary-amd64/Packages.gz$' "${work}/Release" || die "Release does not list main/binary-amd64/Packages.gz"
sign=(gpg --batch --yes --quiet --local-user "$key" --digest-algo SHA512)
"${sign[@]}" --clearsign --output "${work}/InRelease" "${work}/Release" || die "gpg could not sign Release with ${key}"
"${sign[@]}" --armor --detach-sign --output "${work}/Release.gpg" "${work}/Release" || die "gpg could not sign Release with ${key}"
mv -f -- "${work}/Release" "${work}/InRelease" "${work}/Release.gpg" "$dist/"

# 4. The source file administrators install, with the public key inline:
# deb822 continues a field on lines that start with a space, and writes an
# empty line as " .".
gpg --batch --armor --export --export-options export-minimal -- "$key" >"${work}/key.asc"
grep -q -- '-----BEGIN PGP PUBLIC KEY BLOCK-----' "${work}/key.asc" || die "gpg exported no public key for ${key}"
{
	echo "Types: deb"
	echo "URIs: ${base_url}"
	echo "Suites: stable"
	echo "Components: main"
	echo "Architectures: amd64"
	echo "Signed-By:"
	sed -e 's/^$/./' -e 's/^/ /' "${work}/key.asc"
} >"${work}/linuxio.sources"
mv -f -- "${work}/linuxio.sources" "${out_dir}/linuxio.sources"

echo "repo-apt: ${out_dir} holds $(grep -c '^Package: ' "${index}/Packages") package(s), signed by ${key}" >&2
