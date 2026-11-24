#!/usr/bin/env bash
# Publish the LinuxIO RPMs of a folder into a signed static RPM repository
# (docs/releases.md, "Repositories"; docs/development.md):
#
#   OUT_DIR/linuxio.repo                       gpgcheck=1, repo_gpgcheck=1
#   OUT_DIR/RPM-GPG-KEY-linuxio                the public key
#   OUT_DIR/stable/x86_64/linuxio-<version>-1.x86_64.rpm, signed with rpmsign
#   OUT_DIR/stable/x86_64/repodata/            createrepo_c, updateinfo, repomd.xml.asc
#
# A package already in OUT_DIR stays, so publishing one more version keeps
# the earlier ones; the metadata is generated again over all of them. A
# version already published from another build is refused: clients that read
# the first one would see a checksum mismatch.
#
# KEY names a key already in the caller's GnuPG keyring (GNUPGHOME): a
# passphrase-less signing subkey in CI, a throwaway key in a test VM. This
# script never creates, imports or prints a secret key; it exports only the
# public key, as RPM-GPG-KEY-linuxio. BASE_URL is the URL of OUT_DIR once it
# is published (https://.../rpm, or file:///... in a test VM), which
# linuxio.repo points dnf at.
#
# Usage: repo-rpm.sh --packages-dir DIR --key KEYID --base-url URL --out-dir DIR
set -euo pipefail
umask 022

die() {
	echo "repo-rpm: $*" >&2
	exit 1
}

usage="usage: repo-rpm.sh --packages-dir DIR --key KEYID --base-url URL --out-dir DIR"
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

command -v rpm >/dev/null || die "rpm is missing: install rpm"
command -v rpmsign >/dev/null || die "rpmsign is missing: install rpm-sign"
command -v createrepo_c >/dev/null || die "createrepo_c is missing: install createrepo_c"
command -v modifyrepo_c >/dev/null || die "modifyrepo_c is missing: install createrepo_c"
command -v gpg >/dev/null || die "gpg is missing: install gnupg2"
gpg --batch --list-secret-keys -- "$key" >/dev/null 2>&1 ||
	die "no secret key '${key}' in the GnuPG keyring${GNUPGHOME:+ ${GNUPGHOME}}"

shopt -s nullglob
rpms=("${packages_dir}"/linuxio-*.x86_64.rpm)
[[ ${#rpms[@]} -gt 0 ]] || die "no linuxio-*.x86_64.rpm in ${packages_dir}"

mkdir -p "$out_dir"
out_dir="$(realpath -- "$out_dir")"
arch_dir="${out_dir}/stable/x86_64"
mkdir -p "$arch_dir"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# The digest of an RPM's header without its signatures: the same before and
# after rpmsign, so a package published earlier is recognised.
header_sum() { rpm -qp --nosignature --qf '%{SHA256HEADER}' "$1" 2>/dev/null; }

# 1. The packages, each signed in the repository's copy. rpmsign runs gpg
# without a terminal; the key has no passphrase to ask for.
for file in "${rpms[@]}"; do
	nevra="$(rpm -qp --nosignature --qf '%{NAME} %{VERSION}-%{RELEASE}.%{ARCH}' "$file" 2>/dev/null)" ||
		die "cannot read ${file}"
	read -r name evr <<<"$nevra"
	[[ $name == linuxio && $evr == *.x86_64 ]] || die "${file} is ${nevra}, not linuxio x86_64"
	dest="${arch_dir}/linuxio-${evr}.rpm"
	if [[ -e $dest ]]; then
		[[ "$(header_sum "$file")" == "$(header_sum "$dest")" ]] ||
			die "linuxio ${evr} is already published from another build (${dest})"
		echo "repo-rpm: linuxio ${evr} is already published" >&2
		continue
	fi
	cp -- "$file" "${work}/${dest##*/}"
	rpmsign --addsign \
		--define "_gpg_name ${key}" \
		--define "_gpg_sign_cmd_extra_args --batch" \
		"${work}/${dest##*/}" >&2 || die "rpmsign could not sign ${file} with ${key}"
	rpm -K --nosignature "${work}/${dest##*/}" >/dev/null 2>&1 || die "rpm finds ${file} damaged after signing"
	# Unless rpm's keyring has the key, the signature reads as one of an
	# unknown key (NOKEY, and rpm exits 1): that it is there is what counts.
	# rpm 4 names the key ID, rpm 6 the ID or the fingerprint.
	checked="$(rpm -Kv "${work}/${dest##*/}" 2>&1 || true)"
	grep -qi 'signature, key \(ID\|fingerprint\)' <<<"$checked" || die "rpmsign left ${file} unsigned"
	mv -f -- "${work}/${dest##*/}" "$dest"
	echo "repo-rpm: added linuxio ${evr}" >&2
done

# 2. The metadata over every package in the folder.
createrepo_c --quiet --update --checksum sha256 "$arch_dir" >&2 || die "createrepo_c failed in ${arch_dir}"
repomd="${arch_dir}/repodata/repomd.xml"
[[ -s $repomd ]] || die "createrepo_c wrote no ${repomd}"

# 3. One advisory per package in updateinfo, its %changelog text as the
# description: PackageKit's dnf backend shows an update's notes from there
# and reads no changelog of a package that is not installed. A package from
# before the RPMs carried the notes has no %changelog and says its version
# alone. The type follows the notes' headings: Security, then Bug Fixes, then
# an enhancement.
# The text as XML 1.0 holds it: escaped, and without the control characters
# it forbids (a terminal's escape pasted into the notes would make the whole
# file unreadable, every package's advisory with it).
xml_text() {
	LC_ALL=C tr -d '\000-\010\013\014\016-\037' |
		sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g' -e 's/"/\&quot;/g'
}
{
	echo '<?xml version="1.0" encoding="UTF-8"?>'
	echo '<updates>'
	for file in "${arch_dir}"/linuxio-*.rpm; do
		fields="$(rpm -qp --nosignature --qf '%{NAME} %{EPOCHNUM} %{VERSION} %{RELEASE} %{ARCH} %{SOURCERPM} %{BUILDTIME}' "$file" 2>/dev/null)" ||
			die "cannot read ${file}"
		read -r name epoch version release arch source_rpm built <<<"$fields"
		notes="$(rpm -qp --nosignature --qf '%{CHANGELOGTEXT}' "$file" 2>/dev/null)" || notes=""
		[[ -n $notes && $notes != "(none)" ]] || notes="- LinuxIO ${version}."
		type=enhancement
		if grep -qx 'Security:' <<<"$notes"; then
			type=security
		elif grep -qx 'Bug Fixes:' <<<"$notes"; then
			type=bugfix
		fi
		cat <<EOF
  <update from="packages@engmariz.com" status="stable" type="${type}" version="2.0">
    <id>linuxio-${version}-${release}</id>
    <title>LinuxIO ${version}</title>
    <issued date="$(date -u -d "@${built}" '+%Y-%m-%d %H:%M:%S')"/>
    <severity>None</severity>
    <description>$(xml_text <<<"$notes")</description>
    <pkglist>
      <collection short="linuxio">
        <name>LinuxIO</name>
        <package name="${name}" epoch="${epoch}" version="${version}" release="${release}" arch="${arch}" src="${source_rpm}">
          <filename>${file##*/}</filename>
          <sum type="sha256">$(sha256sum <"$file" | cut -d' ' -f1)</sum>
        </package>
      </collection>
    </pkglist>
  </update>
EOF
	done
	echo '</updates>'
} >"${work}/updateinfo.xml"
rm -f "${arch_dir}"/repodata/*updateinfo*
modifyrepo_c --mdtype=updateinfo --compress-type=gz "${work}/updateinfo.xml" "${arch_dir}/repodata" >&2 ||
	die "modifyrepo_c could not add updateinfo to ${arch_dir}/repodata"
grep -q 'type="updateinfo"' "$repomd" || die "repomd.xml lists no updateinfo"

# 4. The signature of the metadata.
rm -f "${repomd}.asc"
gpg --batch --yes --quiet --local-user "$key" --digest-algo SHA512 --armor --detach-sign \
	--output "${work}/repomd.xml.asc" "$repomd" || die "gpg could not sign ${repomd} with ${key}"
mv -f -- "${work}/repomd.xml.asc" "${repomd}.asc"

# 5. The public key, and the repository file administrators install.
gpg --batch --armor --export --export-options export-minimal -- "$key" >"${work}/RPM-GPG-KEY-linuxio"
grep -q -- '-----BEGIN PGP PUBLIC KEY BLOCK-----' "${work}/RPM-GPG-KEY-linuxio" || die "gpg exported no public key for ${key}"
mv -f -- "${work}/RPM-GPG-KEY-linuxio" "${out_dir}/RPM-GPG-KEY-linuxio"
cat >"${work}/linuxio.repo" <<EOF
[linuxio]
name=LinuxIO
baseurl=${base_url}/stable/\$basearch
enabled=1
gpgcheck=1
repo_gpgcheck=1
gpgkey=${base_url}/RPM-GPG-KEY-linuxio
EOF
mv -f -- "${work}/linuxio.repo" "${out_dir}/linuxio.repo"

count=("${arch_dir}"/linuxio-*.rpm)
echo "repo-rpm: ${out_dir} holds ${#count[@]} package(s), signed by ${key}" >&2
