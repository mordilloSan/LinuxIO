# Releases

A release of LinuxIO is a DEB and an RPM of one version, signed, published in
an APT and a DNF repository served from GitHub Pages, and attached to a GitHub
release. `.github/workflows/release.yml` builds and publishes it; nothing a
release is built from changes unless a commit changes it. How the packages
themselves are laid out and assembled, and what they do when they are
installed, upgraded and removed, is in
[Packages, installation and updates](./packaging.md).

## What a release is built from

Everything a release is built from is named in the repository:

| What | Where | Moved by |
| --- | --- | --- |
| The compile image, which also assembles the DEB | `COMPILE_IMAGE` in `packaging/build/pins.env` | `make update-build-pins` |
| The Fedora image, which assembles the RPM and the signed repositories | `RPM_IMAGE` in `packaging/build/pins.env` | `make update-build-pins` |
| Node | `NODE_VERSION` in `packaging/build/pins.env` | `make update-build-pins` |
| golangci-lint, modernize, deadcode, govulncheck | `*_VERSION` in `packaging/build/pins.env` | `make update-build-pins` |
| Every GitHub Action | its commit in `.github/workflows`, the version in a comment | `make update-build-pins` (and Dependabot) |
| Go | `backend/go.mod` | by hand |
| Go modules | `backend/go.sum` | `make update-go-deps` |
| npm packages | `frontend/package-lock.json` | `make update-deps` |

`packaging/build/pins.env` is `KEY=value` lines: the Makefile reads Node and
the Go tools from it (`make setup` installs exactly those versions, and
reinstalls a Go tool whose build information names another version), and the
workflows read it into their environment. `make localinstall` and the test VMs
build with the developer's own system libraries; only a release is built in
the pinned images.

Each image is named as `distribution:release@sha256:digest`. The digest fixes
the bytes; the release says which distribution release those bytes are, so a
reader can tell what the build machine was without pulling it. The images are
the distributions' own; the build packages (compiler, PAM, systemd headers,
debhelper, rpm-build and the rest) are installed in the job with the image's
package manager, so the distribution release decides their versions, and
within one release the distribution keeps them compatible.

### Why the compile image is the oldest supported system

A program records the oldest version of each system library it can run with,
taken from the machine that linked it. All nine programs need the system's C
library, and `linuxio-auth` also needs PAM, systemd, audit and libcap-ng. A
package built on a newer system than a host has does not run there, so the
programs are compiled on the supported system with the oldest C library:

| System | C library (archive, 2026-11-21) |
| --- | --- |
| Ubuntu 24.04 LTS | 2.39 (`libc6` 2.39-0ubuntu8.12 in `noble-updates`) |
| Debian 13 (stable) | 2.41 (`libc6` 2.41-12+deb13u5) |
| Fedora 44 | 2.43 (`glibc` 2.43-2.fc44) |
| Fedora 45 | 2.44 (`glibc` 2.44-2.fc45) |

So the compile image is Ubuntu 24.04. `make update-build-pins` moves its
digest within 24.04 and never its release: the release moves only when the
oldest supported system changes. The RPM is assembled from the same programs,
byte for byte, so its C library floor is Ubuntu 24.04's too; the Fedora image
only runs `rpmbuild`, `rpmsign` and `createrepo_c`, and follows Fedora's
current release (the one Docker Hub tags `latest`).

### `make update-build-pins`

`scripts/update-build-pins.sh` looks up, with curl, jq and git only (no sudo,
no docker):

- the digest Docker Hub serves for each image's release, and for the Fedora
  image whether `latest` has moved to the next release;
- the newest Node release in the major `NODE_VERSION` names (a new major is
  an edit of `packaging/build/pins.env` and `frontend/package.json` by hand);
- the newest release of each Go tool on the Go module proxy, kept back when
  it needs a newer Go than `backend/go.mod`;
- the newest `vX.Y.Z` tag of every action the workflows use, and its commit
  (a new major is moved too, and marked in the commit message). A `uses:`
  line that names a branch is left as it is and named in a note: the Codacy
  scan calls the owner's shared workflow at `mordilloSan/.github@main`, which
  is kept current in that repository and is not part of a release's build.

It makes every lookup before it writes anything, so a registry or GitHub it
cannot reach fails the target with the host named and changes nothing. Then it
rewrites the pins and the `uses:` lines, shows the diff of what it changed and
prints a commit message; with nothing to move it prints "Build pins are
current". The new pins take effect in the release at the next release; run
`make setup` and `make test-quiet` before committing them.

## The release workflow

`gh release-flow merge` merges `dev/vX.Y.Z` into `main`. The workflow runs
twice for one release:

1. The `pull_request` run of the merge (job `tag`) tags the merge commit
   `vX.Y.Z`, refuses a tag that already points elsewhere, starts the workflow
   again with `workflow_dispatch` on the tag with `publish` ticked, and waits
   for that run, so `gh release-flow` sees the release's outcome. A
   `workflow_dispatch` started with the workflow's own token is the one kind
   of run GitHub lets a workflow start.
2. The `workflow_dispatch` run on the tag is the release. Its ref is
   `refs/tags/vX.Y.Z`, which is what the two environments check.

The release run's jobs, in order; each needs the ones before it, so a
failure anywhere, in either package format, stops everything after it:

| Job | Runs in | Does | Permissions | Secrets and environment |
| --- | --- | --- | --- | --- |
| `prepare` | `ubuntu-latest` (reads files only) | Reads `packaging/build/pins.env`; checks that a publishing run's ref is a `vX.Y.Z` tag | `contents: read` | none |
| `compile` | `COMPILE_IMAGE` | Installs the build packages; `make ci-frontend-deps build-vite build-binaries`; `make package-deb`; `make test-packages-quiet`; keeps the programs as one tar (modes and bytes intact) and the DEB | `contents: read` | none |
| `rpm` | `RPM_IMAGE` | Unpacks the compile job's programs; `make package-rpm`; `make test-packages-quiet`; keeps the RPM | `contents: read` | none |
| `repositories` | `RPM_IMAGE` | Fetches the kept earlier packages; imports the signing subkey into a GnuPG home of its own; `make repo-apt repo-rpm`; checks every signature against `packaging/keys/linuxio.asc`; deletes the GnuPG home; keeps the tree as the Pages artifact and this version's packages as the release assets | `contents: read` | environment `release`, `LINUXIO_SIGNING_KEY` |
| `deploy` | `ubuntu-latest` (calls the Pages API) | `actions/deploy-pages` with the tree | `pages: write`, `id-token: write` | environment `github-pages` |
| `publish` | `ubuntu-latest` (calls the Releases API) | Writes the release notes with `.github/scripts/changelog-entry.sh`; creates a draft release with the DEB, the signed RPM and `SHA256SUMS`, then publishes it | `contents: write` | none |

The workflow's default permissions are none. The package tests run before any
job that can see the signing key, and the GitHub release is published only
after the Pages deployment succeeded. The version comes from
`packaging/scripts/package-version.sh`; a publishing run fails when it is not
the tag's version. One publishing run is in progress at a time and a running
one is never cancelled; GitHub keeps one more waiting, and a third replaces
the waiting one.

Dispatched by hand without `publish` (on any branch or tag), the workflow runs
`prepare`, `compile` and `rpm` and stops: a dry run of the build and the
package tests that touches no key, no Pages and no release. With `publish` on
an existing tag, it releases that tag again; a release already published is
never replaced (its draft is).

## Repositories

`make repo-apt` and `make repo-rpm` build one tree, `build/repo`, with
`apt/` and `rpm/` beside each other, signed with the key the job imported, its
URLs under `https://` and the name `PACKAGES_DOMAIN` at the top of the
workflow. The whole tree is one Pages artifact, so the two repositories always
change together.

GitHub Pages serves at most 1 GB per site, and a deployment taking longer than
10 minutes fails
([GitHub Pages limits](https://docs.github.com/en/pages/getting-started-with-github-pages/github-pages-limits)).
The nine programs are 92 MB; compressed as the packages compress them they
are 29 MB (xz) to 31 MB (zstd), so one version, a DEB and an RPM, is about
60-65 MB (measured from the programs, not from built packages). The tree keeps
the last `KEPT_RELEASES` versions, 5 (about 330 MB), which leaves room for
the programs to grow threefold; older packages stay on GitHub Releases.

Pages keeps no files between deployments, so each release rebuilds the tree:
`packaging/build/previous-packages.sh` downloads the DEB and the RPM of the
newest `KEPT_RELEASES - 1` other published, non-prerelease `vX.Y.Z` releases,
checks each against the `SHA256SUMS` of its own release, and passes over a
release without packages. The first release, with no earlier packaged one,
publishes a tree with itself alone. The signed RPM in the tree is the one
attached to the release, so an earlier release's RPM comes back already
signed.

## Trust

- The OpenPGP primary key only certifies. The owner keeps it as an
  encrypted backup away from any machine, and no machine holds it between
  the times a subkey is made.
  A signing subkey, the only secret CI has, signs the APT `Release` files,
  `repomd.xml` and every RPM. The public key is `packaging/keys/linuxio.asc`;
  the `repositories` job refuses a subkey that does not belong to it, and
  checks every signature against that file, not against the keyring that
  signed.
- The subkey is the environment secret `LINUXIO_SIGNING_KEY` of the
  environment `release`, without a passphrase: a passphrase stored in the
  same environment would protect nothing the environment does not already
  protect, and `rpmsign` would need loopback pinentry to use one. It is
  imported only into a GnuPG home under the job's temporary folder, which
  the job deletes whether it succeeds or not.
- `release` admits only `v*` tags and waits for a required reviewer, with
  administrator bypass off. `github-pages` admits only `v*` tags (and the
  default branch, which nothing deploys from).

Pull requests and forks cannot reach the key:

- Environment rules are checked against the run's `GITHUB_REF`. A
  `pull_request` run's ref is `refs/pull/N/merge`, never a tag, so whatever
  workflow a pull request carries, a job of its run that names `release` is
  refused before it starts, and environment secrets are given only to jobs
  that name their environment and are admitted.
- A run for a fork's pull request has a read-only token and no secrets at
  all, so it can neither push a tag nor start a `workflow_dispatch` run.
  Starting a `workflow_dispatch` run by hand needs write access to the
  repository.
- The `tag` job runs only for a merged pull request from this repository's
  own `dev/v*` branch, and its token can push a tag and start a run but
  holds no secret.
- Anyone who can push a `v*` tag can start a run that asks for `release`;
  that run still waits for the reviewer, who sees the tag and commit it is
  for.

## Owner setup

Done once, before the first release. `OWNER/REPO` is `mordilloSan/LinuxIO`.

1. **Keys**, on the owner's own machine, in a folder of their own that is
   deleted at the end, so the primary key is on that machine only while the
   keys are made:

   ```sh
   export GNUPGHOME=~/linuxio-keys
   export GPG_TTY=$(tty)
   install -d -m 0700 "$GNUPGHOME"
   cd "$GNUPGHOME"
   gpg --batch --passphrase '' --quick-generate-key 'LinuxIO packages <packages@engmariz.com>' ed25519 cert 10y
   FPR=$(gpg --with-colons --list-keys packages@engmariz.com | awk -F: '$1 == "fpr" { print $10; exit }')
   gpg --batch --passphrase '' --quick-add-key "$FPR" ed25519 sign 3y
   SUB=$(gpg --with-colons --list-keys "$FPR" | awk -F: '$1 == "sub" { s = 1 } s && $1 == "fpr" { print $10; exit }')
   gpg --armor --export "$FPR" >linuxio.asc
   gpg --armor --export-secret-subkeys "${SUB}!" >linuxio-signing-subkey.asc
   gpg --armor --export-secret-keys "$FPR" | gpg --symmetric --cipher-algo AES256 -o linuxio-primary-secret.asc.gpg
   cp "openpgp-revocs.d/${FPR}.rev" linuxio-revocation.rev
   ```

   The keys are made without a passphrase, and the last `gpg` asks for one:
   it encrypts the backup of the primary key. `GPG_TTY` names the terminal
   that question appears on; without it, over SSH or in an editor's
   terminal, `gpg` ends with "Inappropriate ioctl for device". Where the
   question's full-screen box cannot be drawn ("Screen or window too small"),
   `--pinentry-mode loopback` on that `gpg` asks on the terminal's own line,
   once and with no confirmation, so the check in step 4 matters. Three files
   leave the folder:

   - `linuxio.asc`, the public key, for the repository (step 2);
   - `linuxio-signing-subkey.asc`, the signing subkey alone, for the CI
     secret (step 4);
   - `linuxio-primary-secret.asc.gpg` and `linuxio-revocation.rev`, the
     backup: on two media that are not this machine (a USB stick, a password
     manager), with the passphrase kept apart from them.

   Ed25519 is read by every verifier the supported systems use (GnuPG,
   Sequoia's `sqv` in APT, rpm). Expiry: the primary key 10 years, the
   signing subkey 3 years. Every host holds a copy of the public key, so an
   expiry date, extended or replaced, reaches hosts only when their copy is
   replaced; before the subkey expires, restore the backup into a new folder
   (`gpg --decrypt linuxio-primary-secret.asc.gpg | gpg --import`), add the
   next subkey (`gpg --quick-add-key "$FPR" ed25519 sign 3y`), commit the new
   public key and replace the secret below.

   Stricter, for an owner who does not trust the machine: the same commands
   on a live system without network, with the folder on the encrypted medium
   that holds the backup.

2. **The public key**: commit `linuxio.asc` as `packaging/keys/linuxio.asc`,
   and write its full fingerprint (`gpg --fingerprint "$FPR"`) here:

   Fingerprint: `E16F 7C6E AB03 26F6 47F3  E1A2 2981 530E F4C2 7A54`
   (`LinuxIO packages <packages@engmariz.com>`, made 2026-11-21; the primary
   key expires 2036-11-18 and the signing subkey 2029-11-20)

3. **The `release` environment** (Settings, Environments; or):

   ```sh
   gh api -X PUT repos/OWNER/REPO/environments/release \
     -F 'reviewers[][type]=User' -F "reviewers[][id]=$(gh api user --jq .id)" \
     -F can_admins_bypass=false -F prevent_self_review=false \
     -F 'deployment_branch_policy[protected_branches]=false' \
     -F 'deployment_branch_policy[custom_branch_policies]=true'
   gh api -X POST repos/OWNER/REPO/environments/release/deployment-branch-policies -f name='v*' -f type=tag
   ```

   Self-review stays allowed while the owner is the only reviewer: the owner
   both merges the release and approves it. With a second maintainer, turn
   it on.

4. **The CI secret**: the signing subkey alone, without a passphrase, the
   file step 1 wrote. Check it first: in a GnuPG home that holds nothing
   else, `gpg --list-secret-keys` must show `sec#` (no primary secret) and
   one `ssb`.

   ```sh
   check=$(mktemp -d)
   GNUPGHOME=$check sh -c 'gpg --quiet --import linuxio-signing-subkey.asc && gpg --list-secret-keys; gpgconf --kill all'
   rm -rf "$check"
   gh secret set LINUXIO_SIGNING_KEY --env release --repo OWNER/REPO <linuxio-signing-subkey.asc
   ```

   Then, once the backup of step 1 is on its two media and reads back
   (`gpg --no-symkey-cache --pinentry-mode loopback --decrypt
   linuxio-primary-secret.asc.gpg | head -n 1` asks for the passphrase,
   which GnuPG would otherwise remember from a moment before, and prints
   `-----BEGIN PGP PRIVATE KEY BLOCK-----`), delete the folder of step 1 with
   everything in it:

   ```sh
   gpgconf --kill all
   cd ~ && rm -rf ~/linuxio-keys
   unset GNUPGHOME
   ```

5. **Pages**: source GitHub Actions, then the custom domain and HTTPS:

   ```sh
   gh api -X POST repos/OWNER/REPO/pages -f build_type=workflow
   gh api -X POST repos/OWNER/REPO/environments/github-pages/deployment-branch-policies -f name='v*' -f type=tag
   gh api -X PUT repos/OWNER/REPO/pages -f cname=packages.engmariz.com
   ```

   With a workflow deployment Pages ignores a `CNAME` file; the domain is
   this setting. Tick "Enforce HTTPS" (`gh api -X PUT repos/OWNER/REPO/pages
   -F https_enforced=true`) once GitHub has issued the certificate, which can
   take up to a day after the DNS record resolves.

6. **DNS**, at the provider of `engmariz.com`: one record,
   `packages CNAME mordillosan.github.io.` Verifying `engmariz.com` for the
   account first (GitHub Settings, Pages, "Add a domain": one `TXT` record)
   keeps anyone else's Pages site from taking the name.

7. **Releases that cannot change**, once the first release has gone
   through: turn on release immutability (Settings, General, Releases), and
   add a tag ruleset for `v*` that blocks updates and deletions, so a
   published release's tag and assets stay what was signed. Not before: a
   release run that fails after its tag is pushed is tried again by deleting
   the tag, which the ruleset refuses, and the version would then be lost.

8. **A dry run**: `gh workflow run release.yml --ref main` builds both
   packages in the pinned images and tests them, and stops before the key.

The name `packages.engmariz.com` is `PACKAGES_DOMAIN` at the top of
`.github/workflows/release.yml`; a different name changes that line, the
Pages setting and the DNS record, and must happen before anyone else installs,
since hosts save the address.

## Known limits

- Pages has a soft bandwidth limit of 100 GB a month, about 3,000 downloads
  of a 30 MB package.
- The `tag` job waits up to six hours for the release run, approval
  included; past that the release run goes on, but `gh release-flow merge`
  reports a failure.
- `previous-packages.sh` looks at the newest 100 releases.
