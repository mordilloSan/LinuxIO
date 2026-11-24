#!/usr/bin/env bash
# Fixture checks for repo-apt.sh and repo-rpm.sh, the signed repositories a
# release publishes. Everywhere: the arguments each refuses, and a key the
# keyring does not hold. Where the tools are present (apt-ftparchive and
# dpkg-deb; rpmbuild, rpmsign and createrepo_c), a round trip with a
# throwaway key in a temporary GnuPG home: two versions published one after
# the other are both kept, the metadata's signatures verify against the
# public key the client file carries, the index is reachable by its SHA-256,
# each package's changelog is where apt changelog and PackageKit read it
# (the APT Changelogs field, the RPM updateinfo), and a published version
# rebuilt with other contents is refused. The test
# VMs publish real packages and install from them with apt, dnf and
# PackageKit (package-repository.spec.ts, jobs-update.spec.ts).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_APT="${SCRIPT_DIR}/../repo-apt.sh"
REPO_RPM="${SCRIPT_DIR}/../repo-rpm.sh"
CHANGELOG_ENTRY="${SCRIPT_DIR}/../changelog-entry.sh"
NOTES="${SCRIPT_DIR}/release-notes.md"

fail() {
	echo "❌ $*" >&2
	exit 1
}

pass() {
	printf '   \033[1;32m✓\033[0m %s\n' "$1"
}

skip() {
	printf '   \033[1;33m-\033[0m %s\n' "$1"
}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "${work}/packages"

# refuses MESSAGE SCRIPT ARGS...: SCRIPT fails, saying MESSAGE.
refuses() {
	local want=$1 out
	shift
	if out=$("$@" 2>&1); then
		fail "$(basename "$2") accepted: ${*:2}"
	fi
	[[ $out == *"$want"* ]] || fail "$(basename "$2"): expected '${want}', got: ${out}"
}

for script in "$REPO_APT" "$REPO_RPM"; do
	name=$(basename "$script")
	refuses "usage: ${name}" bash "$script" --packages-dir "${work}/packages" --key K --base-url https://example.invalid/x
	refuses "unknown argument --sign" bash "$script" --sign
	refuses "is not an https://" bash "$script" --packages-dir "${work}/packages" --key K --base-url ftp://example.invalid/x --out-dir "${work}/out"
	refuses "does not exist" bash "$script" --packages-dir "${work}/none" --key K --base-url https://example.invalid/x --out-dir "${work}/out"
done
pass "both scripts refuse missing, unknown and wrong arguments"

apt_tools=0 rpm_tools=0
if command -v gpg >/dev/null && command -v gpgv >/dev/null; then
	command -v apt-ftparchive >/dev/null && command -v dpkg-deb >/dev/null && apt_tools=1
	command -v rpmbuild >/dev/null && command -v rpmsign >/dev/null && command -v createrepo_c >/dev/null && rpm_tools=1
fi
((apt_tools == 0)) && skip "no gpg, gpgv, apt-ftparchive or dpkg-deb: the APT round trip is not run"
((rpm_tools == 0)) && skip "no gpg, gpgv, rpmbuild, rpmsign or createrepo_c: the RPM round trip is not run"
((apt_tools + rpm_tools > 0)) || exit 0

export GNUPGHOME="${work}/gnupg"
install -d -m 0700 "$GNUPGHOME"
((apt_tools == 0)) || refuses "no secret key 'nobody@example.invalid'" bash "$REPO_APT" --packages-dir "${work}/packages" \
	--key nobody@example.invalid --base-url https://example.invalid/x --out-dir "${work}/out"
((rpm_tools == 0)) || refuses "no secret key 'nobody@example.invalid'" bash "$REPO_RPM" --packages-dir "${work}/packages" \
	--key nobody@example.invalid --base-url https://example.invalid/x --out-dir "${work}/out"
pass "a key the keyring does not hold is refused"

uid='LinuxIO fixture <fixture@linuxio.invalid>'
gpg --batch --quiet --passphrase '' --quick-gen-key "$uid" rsa3072 sign never 2>/dev/null
key=$(gpg --batch --with-colons --list-secret-keys "=${uid}" 2>/dev/null | awk -F: '$1 == "fpr" { print $10; exit }')
[[ -n $key ]] || fail "could not make the fixture key"

# --- APT ---------------------------------------------------------------------
if ((apt_tools)); then
	# fake_deb VERSION TEXT [no-changelog]: a linuxio_VERSION_amd64.deb in
	# packages/ alone, with the changelog of the fixture notes. A rebuild has
	# the same bytes: one changelog per version, whose date is in its text,
	# and SOURCE_DATE_EPOCH for the times dpkg-deb writes.
	fake_deb() {
		local root="${work}/deb-root"
		rm -rf "$root" "${work}/packages"/*
		mkdir -p "${root}/DEBIAN" "${root}/usr/share/linuxio" "${root}/usr/share/doc/linuxio"
		printf 'Package: linuxio\nVersion: %s\nArchitecture: amd64\nMaintainer: fixture <fixture@linuxio.invalid>\nDescription: fixture\n' "$1" >"${root}/DEBIAN/control"
		echo "$2" >"${root}/usr/share/linuxio/fixture"
		if [[ ${3:-} != no-changelog ]]; then
			[[ -f "${work}/changelog-${1}.gz" ]] ||
				bash "$CHANGELOG_ENTRY" deb --version "$1" --notes "$NOTES" | gzip -9n >"${work}/changelog-${1}.gz"
			cp "${work}/changelog-${1}.gz" "${root}/usr/share/doc/linuxio/changelog.gz"
		fi
		SOURCE_DATE_EPOCH=1700000000 dpkg-deb --root-owner-group --build "$root" "${work}/packages/linuxio_${1}_amd64.deb" >/dev/null
	}
	out="${work}/apt"
	publish_apt() { bash "$REPO_APT" --packages-dir "${work}/packages" --key "$key" --base-url https://example.invalid/apt/ --out-dir "$out" 2>/dev/null; }
	fake_deb 1.0.0 one
	publish_apt
	fake_deb 1.0.1 two
	publish_apt
	[[ -f "${out}/pool/main/l/linuxio/linuxio_1.0.0_amd64.deb" && -f "${out}/pool/main/l/linuxio/linuxio_1.0.1_amd64.deb" ]] ||
		fail "the APT pool does not keep both versions"
	index="${out}/dists/stable/main/binary-amd64"
	[[ $(grep -c '^Package: linuxio$' "${index}/Packages") == 2 ]] || fail "Packages does not list both versions"
	cmp -s <(gzip -dc "${index}/Packages.gz") "${index}/Packages" || fail "Packages.gz is not Packages"
	pass "a second version joins the first, and the index lists both"

	release="${out}/dists/stable/Release"
	grep -qx 'Acquire-By-Hash: yes' "$release" || fail "Release has no Acquire-By-Hash: yes"
	grep -qx 'Suite: stable' "$release" || fail "Release is not the stable suite"
	! grep -Eq '^(MD5Sum|SHA1|SHA512):' "$release" || fail "Release lists sums other than SHA256"
	for file in Packages Packages.gz; do
		sum=$(sha256sum <"${index}/${file}" | cut -d' ' -f1)
		grep -Eq "^ ${sum} +[0-9]+ main/binary-amd64/${file}$" "$release" || fail "Release does not list the SHA-256 of ${file}"
		cmp -s "${index}/by-hash/SHA256/${sum}" "${index}/${file}" || fail "${file} is not under by-hash/SHA256/${sum}"
	done
	pass "Release lists the index by SHA-256 only, and the index is under by-hash"

	# apt fills @CHANGEPATH@ with component/l/source/source_version.
	grep -qx 'Changelogs: https://example.invalid/apt/changelogs/@CHANGEPATH@_changelog' "$release" ||
		fail "Release has no Changelogs field under the base URL"
	for v in 1.0.0 1.0.1; do
		changelog="${out}/changelogs/main/l/linuxio/linuxio_${v}_changelog"
		[[ -f $changelog ]] || fail "no changelog of ${v} at ${changelog#"${out}"/}"
		[[ $(head -n 1 "$changelog") == "linuxio (${v}) unstable; urgency=medium" ]] || fail "the changelog of ${v} is not its own"
	done
	grep -qxF "  * updates: show the package's changes on the Updates page (abc1234)" "$changelog" ||
		fail "the changelog does not carry the notes as text"
	pass "Release's Changelogs field leads to each package's changelog"

	sources="${out}/linuxio.sources"
	grep -qx 'URIs: https://example.invalid/apt' "$sources" || fail "linuxio.sources does not point at the base URL"
	! grep -q 'trusted' "$sources" || fail "linuxio.sources marks the repository trusted"
	# The key, as apt reads it from Signed-By: the field's lines without their
	# leading space, " ." an empty line.
	sed -n '/^Signed-By:/,$p' "$sources" | tail -n +2 | sed -e 's/^ //' -e 's/^\.$//' >"${work}/apt-key.asc"
	gpg --batch --quiet --dearmor <"${work}/apt-key.asc" >"${work}/apt-key.gpg"
	gpgv --keyring "${work}/apt-key.gpg" "${out}/dists/stable/InRelease" 2>/dev/null || fail "InRelease does not verify against the key in linuxio.sources"
	gpgv --keyring "${work}/apt-key.gpg" "${out}/dists/stable/Release.gpg" "$release" 2>/dev/null || fail "Release.gpg does not verify"
	sed -i 's/^Suite: stable$/Suite: stab1e/' "$release"
	if gpgv --keyring "${work}/apt-key.gpg" "${out}/dists/stable/Release.gpg" "$release" 2>/dev/null; then
		fail "Release.gpg verifies a changed Release"
	fi
	pass "InRelease and Release.gpg verify against the key linuxio.sources carries, and not once Release changes"

	fake_deb 1.0.1 other
	if publish_apt; then fail "a published version was replaced with other contents"; fi
	fake_deb 1.0.1 two
	publish_apt || fail "publishing the same package again failed"
	pass "a published version is not replaced by other contents"

	fake_deb 1.0.2 three no-changelog
	if publish_apt; then fail "a package without a changelog was published"; fi
	[[ ! -e "${out}/pool/main/l/linuxio/linuxio_1.0.2_amd64.deb" ]] || fail "a package without a changelog went into the pool"
	pass "a package without a changelog is refused before it reaches the pool"
fi

# --- RPM ---------------------------------------------------------------------
if ((rpm_tools)); then
	# fake_rpm VERSION TEXT [notes]: a linuxio-VERSION-1.x86_64.rpm in
	# packages/ alone; with notes, its %changelog is the fixture notes', as
	# linuxio.spec reads it.
	fake_rpm() {
		rm -rf "${work}/rpmbuild" "${work}/packages"/*
		local changelog=""
		if [[ ${3:-} == notes ]]; then
			bash "$CHANGELOG_ENTRY" rpm --version "$1" --notes "$NOTES" >"${work}/changelog"
			changelog="%changelog
%(cat ${work}/changelog)"
		fi
		cat >"${work}/fixture.spec" <<EOF
Name: linuxio
Version: $1
Release: 1
Summary: fixture
License: Apache-2.0
%description
fixture
%install
mkdir -p %{buildroot}/usr/share/linuxio
echo $2 >%{buildroot}/usr/share/linuxio/fixture
%files
/usr/share/linuxio/fixture
${changelog}
EOF
		rpmbuild -bb --quiet --target x86_64 --define "_topdir ${work}/rpmbuild" "${work}/fixture.spec" >/dev/null 2>&1 ||
			fail "rpmbuild could not build the fixture package"
		cp "${work}"/rpmbuild/RPMS/x86_64/linuxio-"$1"-1.x86_64.rpm "${work}/packages/"
	}
	out="${work}/rpm"
	publish_rpm() { bash "$REPO_RPM" --packages-dir "${work}/packages" --key "$key" --base-url https://example.invalid/rpm --out-dir "$out" 2>/dev/null; }
	fake_rpm 1.0.0 one
	publish_rpm
	fake_rpm 1.0.1 two notes
	publish_rpm
	for v in 1.0.0 1.0.1; do
		file="${out}/stable/x86_64/linuxio-${v}-1.x86_64.rpm"
		[[ -f $file ]] || fail "the RPM repository does not keep ${v}"
		# rpm exits 1 for a signature of a key its keyring lacks.
		checked=$(rpm -Kv "$file" 2>&1 || true)
		grep -qi 'signature, key' <<<"$checked" || fail "${file##*/} is not signed"
	done
	pass "a second version joins the first, each signed"
	repomd="${out}/stable/x86_64/repodata/repomd.xml"
	gpg --batch --quiet --dearmor <"${out}/RPM-GPG-KEY-linuxio" >"${work}/rpm-key.gpg"
	gpgv --keyring "${work}/rpm-key.gpg" "${repomd}.asc" "$repomd" 2>/dev/null || fail "repomd.xml.asc does not verify against RPM-GPG-KEY-linuxio"
	# shellcheck disable=SC2016 # dnf expands $basearch, not the shell
	for line in 'baseurl=https://example.invalid/rpm/stable/$basearch' gpgcheck=1 repo_gpgcheck=1 'gpgkey=https://example.invalid/rpm/RPM-GPG-KEY-linuxio'; do
		grep -qxF "$line" "${out}/linuxio.repo" || fail "linuxio.repo has no line ${line}"
	done
	pass "repomd.xml.asc verifies against the published key, and linuxio.repo checks both signatures"

	updateinfo=$(sed -n 's|.*<location href="\(repodata/[^"]*updateinfo\.xml\.gz\)".*|\1|p' "$repomd")
	[[ -n $updateinfo ]] || fail "repomd.xml lists no gzip updateinfo"
	gzip -dc "${out}/stable/x86_64/${updateinfo}" >"${work}/updateinfo.xml"
	if command -v xmllint >/dev/null; then
		xmllint --noout "${work}/updateinfo.xml" || fail "updateinfo is not well-formed XML"
	fi
	[[ $(grep -c '<update ' "${work}/updateinfo.xml") == 2 ]] || fail "updateinfo does not hold one advisory per package"
	grep -q 'type="bugfix"' "${work}/updateinfo.xml" || fail "the advisory of notes with Bug Fixes is not a bugfix"
	grep -qF -- "- updates: show the package's changes on the Updates page (abc1234)" "${work}/updateinfo.xml" ||
		fail "the advisory does not carry the package's %changelog"
	grep -qF -- '- memory: keep 50% of %{name} free (def5678)' "${work}/updateinfo.xml" ||
		fail "rpm expanded a macro of the notes"
	grep -qF -- '- #42 stays as it was' "${work}/updateinfo.xml" || fail "rpm dropped a line of the notes"
	grep -qF -- '- %post stays text' "${work}/updateinfo.xml" || fail "rpm read a line of the notes as a section"
	grep -qF -- '- LinuxIO 1.0.0.' "${work}/updateinfo.xml" || fail "a package without %changelog does not name its version"
	pass "updateinfo holds each package's %changelog, as written"

	fake_rpm 1.0.1 other
	if publish_rpm; then fail "a published version was replaced by another build"; fi
	pass "a published version is not replaced by another build"
fi
