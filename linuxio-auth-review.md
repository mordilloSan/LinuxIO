# Account 1 — Original Code Review: `linuxio-auth.c`

**Date:** 2026-08-03
**File:** `backend/auth/linuxio-auth.c` (2,123 lines, branch `dev/v0.17.0`)
**Focus:** performance and clean idiomatic C (per request); correctness and security findings reported as encountered.
**Method:** 4 specialized reviewers (correctness, security, performance, idiomatic C) + 6 adversarial verifiers. 26 raw findings deduplicated to 24; all 24 confirmed against the actual code. Unused-include and `safe_vsnprintf` claims additionally verified by compiling.

**TLDR:** The file is structurally sound — the FD choreography in `spawn_bridge_process`, the fd-based TOCTOU-resistant exec, and the fail-closed sudo probe are all correct on the happy path. The real opportunities: two login-latency wins worth ~100–200 ms each, one large idiomatic cleanup (the duplicated PAM cleanup ladder), two genuine bugs (one breaks root logins on some systems), and a handful of easy deletions.

---

## 1. Performance

Login latency is the only metric that matters here: the process is single-shot (one per connection), so startup cost is amortized over exactly one login.

### 1.1 `usleep(100ms)` poll loop costs every login ~50–100 ms of pure sleep — **high**

`linuxio-auth.c:919` — `run_cmd_as_user_with_input` waits for the sudo child with `waitpid(WNOHANG)` + `usleep(100 * 1000)`. Completion is quantized to 100 ms boundaries: a `sudo -l` that finishes in 30 ms still costs 100 ms; one that finishes in 110 ms costs 200 ms. Average ~50 ms added, worst case ~100 ms, paid on the critical path of **every** password login — and paid a second time by the `sudo -k` call on privileged logins (where the ~10 ms child always costs a full 100 ms quantum). There is no SIGCHLD handler, so the sleep is never interrupted early.

**Fix:** event-driven wait with the same timeout:

```c
int pfd = (int)syscall(SYS_pidfd_open, pid, 0);   /* Linux 5.3+ */
/* poll({.fd = pfd, .events = POLLIN}, 1, timeout_sec * 1000);
   POLLIN  -> waitpid(pid, &status, 0)
   timeout -> existing SIGKILL + reap fail-closed path */
```

Fall back to the current loop (ideally with a 2–5 ms sleep) only on `ENOSYS` — the same syscall-with-fallback idiom the file already uses for `execveat` (line 1543) and `close_range` (line 1512).

### 1.2 The synchronous `sudo -k` second fork is deletable outright — **medium**

`linuxio-auth.c:971` — after a successful probe, a second subprocess (`sudo -k`) runs synchronously before the login can proceed; under the current poll loop it reliably adds ~100 ms to every privileged login.

**Fix:** add `-k` to the probe argv itself (line 954):

```c
{"/usr/bin/sudo", "-k", "-S", "-p", "", "-u", "root", "-l", "--", BRIDGE_PATH, NULL}
```

Per sudo(8), `-k` used in conjunction with a command or `-l` makes sudo **neither use nor update** the credential cache — no ticket is ever created, so the second `run_cmd_as_user_with_input` call (lines 968–973) can be deleted. This is also strictly better security: the current code leaves a ticket behind on the `rc != 0` path (user authenticated but policy denied the bridge), which never gets invalidated.

### 1.3 sudo probe fully serialized with bridge setup and `pam_open_session` — **low**

`linuxio-auth.c:1721` — the probe completes before `open_and_validate_bridge`, pipe creation, and `pam_open_session` begin, yet `want_privileged` is only consumed at spawn time (line 1819) and in the bootstrap payload. `pam_open_session` can cost 10–50 ms when pam_systemd does its synchronous D-Bus round-trip to logind; the two waits are strictly additive today.

**Fix:** fork the probe child right after PAM auth succeeds, do bridge validation / pipe setup / `pam_open_session` while it runs, then collect its exit status (poll on pidfd with the existing timeout) just before deciding the mode. Error paths must kill/reap the probe child.

### 1.4 utmp/wtmp/lastlog recording runs before the OK response — **low**

`linuxio-auth.c:1985` — `record_login_start` runs before `send_ok_response` even though its outcome never affects the response (all failures logged and ignored). It puts a utmp scan and a blocking wtmp lock (glibc: `fcntl(F_SETLKW)` bounded by `alarm(10)`, so up to ~10 s per lock under churn) on the time-to-OK path.

**Fix:** swap the two lines. The parent then blocks in `waitpid` for the bridge's lifetime anyway, so accounting still happens immediately and still strictly precedes `record_login_end`.

---

## 2. Clean idiomatic C

### 2.1 PAM cleanup ladder duplicated across 16 error paths — **high**

`handle_client` (line 1567) repeats the manual cleanup ladder on every error path. Verified counts: `pam_end` at 17 sites, `pam_setcred(PAM_DELETE_CRED)` at 14, `pam_close_session` at 9, `secure_bzero(password)` at 11, plus 4 pre-PAM paths that repeat send+bzero+return. Which subset applies changes four times as fds are opened and handed off, so every new error path must reproduce a 3–6 line ladder correctly. The file already uses `goto`-cleanup in `update_lastlog` (line 467), so an epilogue matches existing conventions.

**Fix:** single `goto out` epilogue with state flags:

```c
int ret = 1;
int session_open = 0, cred_set = 0;
int bridge_fd = -1, bootstrap_pipe[2] = {-1, -1}, exec_status_pipe[2] = {-1, -1};
/* each failure: { send_error_response(output_fd, CODE, msg); goto out; }
   set cred_set = 1 after pam_setcred(ESTABLISH),
   session_open = 1 after pam_open_session,
   reset fds to -1 as they are intentionally closed / handed off */
out:
  /* close each fd >= 0 */
  if (session_open) pam_close_session(pamh, 0);
  if (cred_set)     pam_setcred(pamh, PAM_DELETE_CRED);
  if (pamh)         pam_end(pamh, 0);
  secure_bzero(password, sizeof(password));
  return ret;
```

Two caveats from verification:
- **Keep the early wipe at line 1724.** An epilogue-only wipe would hold the plaintext password in the parent's memory for the bridge's entire multi-hour lifetime, since the parent blocks in `waitpid`.
- Three sites (1651, 1669, 1683) currently pass `rc` rather than `0` to `pam_end`; preserve that if it matters to you.

### 2.2 `safe_vsnprintf` re-implements what the build already does — **medium**

Lines 82–109. Three build-variant paths of which only one ever compiles:
- The `vsnprintf_s` branch is dead — glibc does not implement Annex K.
- The hand-written `__builtin___vsnprintf_chk(dst, dstsz, 0, dstsz, ...)` is **weaker** than plain `vsnprintf` under the Makefile's existing `-D_FORTIFY_SOURCE=3` (Makefile:193): passing `dstsz` as the object size makes the fortify check vacuous and forecloses the compiler-computed object-size check.
- The manual re-termination at lines 105–106 is redundant — C99 `vsnprintf` always NUL-terminates when `dstsz > 0`.

**Fix:** collapse the body to:

```c
if (!dst || dstsz == 0) return -1;
int n = vsnprintf(dst, dstsz, fmt, ap);
if (n < 0) { dst[0] = '\0'; return -1; }
return n;
```

and delete `#define __STDC_WANT_LIB_EXT1__ 1` at line 3.

### 2.3 `readlink(/proc/self/fd)` dance to find a compile-time-constant parent dir — **medium**

Lines 719–747 in `open_and_validate_bridge`: format `/proc/self/fd/%d`, readlink into a PATH_MAX buffer, `strrchr`-truncate, re-open the string — but `BRIDGE_PATH`'s parent is statically `/usr/local/bin`. The dance also validates whatever directory the file is in at readlink time (a rename between open and readlink makes the dir check follow the moved location — theoretical, root-only, but the simple form doesn't have the race at all). There's also a duplicated `fstat`: line 707, then again inside `validate_bridge_via_fd` (line 649).

**Fix:** validate the constant parent first, then `openat` the basename relative to it:

```c
int dfd = open("/usr/local/bin", O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
if (dfd < 0) return -1;
int fd = openat(dfd, "linuxio-bridge", O_PATH | O_NOFOLLOW | O_CLOEXEC);
```

then fstat once and pass the `struct stat *` into the existing policy checks. Derive the two strings from `BRIDGE_PATH` with `#define`s for a single source of truth.

### 2.4 Straight deletions (all verified: file compiles warning-free without them) — **low**

| What | Where | Note |
|---|---|---|
| Unused includes | lines 23, 30, 35, 36, 37 | `sys/mman.h`, `strings.h`, `sys/mount.h`, `sched.h`, `ctype.h` — zero uses of any symbol. `time.h` (line 8) is redundant too (`time_t` comes from `sys/types.h`). Keep `utmp.h` and `paths.h` — they are used. A root binary including mount/namespace headers it never uses is audit noise. |
| `#ifndef _WIN32` guard | lines 200, 214 | 15 unguarded `secure_bzero` call sites mean the guard can only ever break a build, never save one. Optionally drop `|| defined(__APPLE__)` at 203; keep the volatile fallback only if musl is intended. |
| `dup_pam_string` | lines 300–312 | Byte-for-byte `strdup`; both call sites (591, 603) already guard against NULL sources, and the existing NULL-result checks handle allocation failure. |
| Triplicated `pipe2` `#if/#else` | 843–857, 1745–1758, 1785–1817 | The `#else` arms are dead on the only compilable target (file unconditionally uses sd-journal, `SO_PEERCRED`, `syscall(__NR_execveat)`). The exec-status site duplicates the entire 8-line PAM error ladder in *both* preprocessor arms. Call `pipe2(p, O_CLOEXEC)` directly. |
| `read_lenstr` temp malloc | lines 264–284 | Creates a *second* in-memory copy of the password rather than reducing exposure as its comment claims, plus a malloc-failure path per field. Read directly into the caller's buffer; wipe `buf` on partial-read failure. |
| `MAX_PATH_LEN` | line 79 | Duplicates `PATH_MAX` (used directly at 719, 1549); single use at line 297. |
| `write_all` after `main` | line 2107 | Move next to `read_all` — but **above `log_stderrf` (line 188)**, its first user, or the static redeclaration won't compile. Delete both forward decls at 75–76 (`env_get_int`'s is already unnecessary). |
| Header comment says "JSON" | line 2 | The protocol is binary (magic + length-prefixed fields per `linuxio_protocol.h`). First thing an auditor reads about a root binary misstates its wire contract. |

---

## 3. Bugs

### 3.1 Root logins break on the unprivileged path — **medium**

`linuxio-auth.c:1006` — `drop_to_user`'s verification `if (setuid(0) == 0) child_die(...)` treats regaining root as proof the drop failed. When the authenticated user **is** root, `setuid(0)` always succeeds, so the child unconditionally dies. Root reaches the unprivileged path whenever `user_can_run_bridge_as_root()` returns 0: `/usr/bin/sudo` not installed (execv fails, rc 127), sudoers without the default root rule, probe timeout, or empty password. On such systems every root login fails with an opaque "bridge exec failed" after PAM already succeeded.

**Fix:** `if (auth_user->uid != 0 && setuid(0) == 0) child_die(...)`, or verify with `getresuid()` that ruid/euid/suid all equal the target uid.

### 3.2 Session identity uses the client-typed username, not `PAM_USER` — **medium**

`linuxio-auth.c:1689` — `getpwnam(user)` uses the raw wire string, but PAM modules may canonicalize the username during `pam_authenticate`/`pam_acct_mgmt` (SSSD case-insensitive domains, `pam_userdb`, LDAP aliasing); sshd/login/cockpit all re-fetch it for this reason. Everything downstream roots at the typed string: bridge identity, HOME/USER env, utmp/wtmp/lastlog, the OK response. Failure modes: lookup fails after successful auth (login rejected), or the session is built for a different account than PAM authenticated. Default local-passwd stacks are unaffected.

**Fix:** after `pam_acct_mgmt` succeeds:

```c
const void *pam_user = NULL;
pam_get_item(pamh, PAM_USER, &pam_user);
/* use pam_user (fallback to the request username only if unset)
   for getpwnam, the sudo probe, utmp records, and the response */
```

### 3.3 Minor

- **`pam_conv_func` incompatible function-pointer cast** (line 1633, declared 575): `const void *appdata_ptr` vs the `void *` libpam expects — formally UB (C11 6.3.2.3p8), works on the SysV ABI, traps under CFI. Change the parameter to `void *` and delete the cast.
- **`--version` compares `argv[0]`** (line 2061): `strcmp(argv[0], "--version")` can never match a normal invocation; reads like a typo. Drop the clause, keep the `argv[1]` checks.
- **`spawn_bridge_process` only rescues a client fd arriving on fd 0** (line 1360): if `client_fd` were 1, Step 3's `dup2(STDERR_FILENO, STDOUT_FILENO)` destroys the client socket before Step 4 copies it; if 2, diagnostics alias into the Yamux stream. Unreachable from the sole caller (always fd 0) — a latent trap given the code claims to handle fds "at any position". Park any `orig_client < CLIENT_CONN_FD` above `BRIDGE_FD` with plain `F_DUPFD` (not CLOEXEC — fd 3 must survive exec).

---

## 4. Security hardening (adjacent findings)

- **Empty passwords reach PAM** (line 1655): field validation only requires `user` and `session_id` to be non-empty, and `pam_authenticate(pamh, 0)` / `pam_acct_mgmt(pamh, 0)` pass no flags. On stacks where pam_unix has `nullok` (common distro defaults included by the `linuxio` service file), blank-password accounts become web-loginable. sshd blocks this class with `PAM_DISALLOW_NULL_AUTHTOK`. Fix: pass that flag to both calls and reject zero-length passwords in the line-1606 check.
- **Username accepted with arbitrary bytes** (line 1606): `remote_host` and `session_id` get character filters; the username gets none. On failed auth the unauthenticated attacker-controlled string goes verbatim into `/var/log/btmp` `ut_user` (line 1681) and journal/PAM syslog fields — terminal escape sequences fire when an admin runs `lastb`. Fix: `valid_username()` mirroring the existing validators (reject `ch <= 0x20` and `ch >= 0x7f`).
- **`execveat` ENOSYS fallback reintroduces the TOCTOU** (lines 1546–1560): it readlinks, **closes the validated fd**, then `execv`s by path — severing the tie to the validated inode on exactly the old kernels that hit the fallback. The documented emulation is `execv("/proc/self/fd/5", ...)` with the fd kept open (or `child_die` if execveat is unavailable). Also: `#define __NR_execveat 322` (line 1541) is the x86-64 number only.
- **`valid_remote_host` admits C1 control bytes 0x80–0x9F** (line 394), including 0x9B (single-byte CSI), which flow into `ut_host`/`ll_host`/`PAM_RHOST`. Verified defense-in-depth only: the Go upstream canonicalizes every attacker-influenced value through `netip.Addr`, so this matters only for other peers already authorized on the socket. Fix: require printable ASCII.

---

## 5. Suggested order of attack

1. **§1.2 + §1.1** — merge `-k` into the probe and switch to pidfd+poll: biggest latency win for the least code (~100 ms every login, ~200 ms privileged).
2. **§3.1** — the root-login bug: the one thing actually broken.
3. **§2.1** — the goto epilogue: biggest readability/maintainability win; do it before touching other error paths.
4. **§2.4** — the deletions: mechanical, zero-risk, shrinks the audit surface.
5. **§4** — `PAM_DISALLOW_NULL_AUTHTOK` + `valid_username()`: small patches, real hardening.
6. The rest as convenient (§2.2, §2.3, §3.2, §3.3, remaining §1 items).

---

# Account 2 — Independent Audit and Reconciliation

- **Date:** 2026-08-03
- **Scope:** Read-only verification of Account 1 against the current launcher,
  its Go caller, PAM and systemd packaging, build/CI coverage, documentation,
  and relevant platform behavior.
- **Source baseline:** branch `dev/v0.17.0`, HEAD
  `688285c314ba1f120694afa0451fffa7c70ae4ac`.
- **Reviewed launcher SHA-256:**
  `2e7f48f3da16d7ce6894a3113a5c06092a7dedd968d2aa2f08f0ae7381ad8228`.

This account intentionally leaves Account 1 above intact. It records a second,
independent assessment so that the original observations and the subsequent
corrections can be evaluated side by side.

## Overall conclusion

Account 1 is a useful source-review draft, but its statement that all 24
findings were confirmed is too strong. Most mechanical observations are valid.
Several conclusions are unmeasured or overstated, the glibc locking description
is outdated, the proposed root fix would violate the launcher's privilege-mode
invariant, and the review misses several failure paths in child supervision, fd
closure, and the exec-status protocol.

The original TLDR is fair only when read narrowly as a statement about the
normal happy path. It should not be read as evidence that the sudo, fd, or
startup machinery is fail-closed on every error path.

## Provenance and evidence limitations

- `linuxio-auth-review.md` is untracked and has no Git history. Its claim of
  four specialized reviewers, six adversarial verifiers, and prior compilation
  has no retained log or repository artifact from which it can be reproduced.
- The review's branch, source path, and 2,123-line count match the audited
  source.
- There are Go protocol and client tests, but no C launcher unit tests or
  PAM/sudo/fd/runtime integration suite.
- CodeQL builds and analyzes the C helper. Release verification builds it and
  checks that it is an ELF executable; it does not exercise authentication,
  PAM identity mapping, sudo policy, fd choreography, timeout handling, or the
  bridge-start handshake.
- The performance figures in Account 1 have no benchmark, trace, or production
  observation in the repository.

## Disposition of all 24 original findings

The eight entries in §2.4 are counted separately, matching Account 1's total of
24 findings.

| ID | Account 1 finding | Independent disposition |
|---|---|---|
| 1.1 | 100 ms sudo polling | **Partly confirmed.** The loop quantizes completion, but the quoted average and per-login cost are unmeasured. It can add zero sleep when the child is already complete. The review also misses a final-boundary race that can kill a child which completed during the last sleep. |
| 1.2 | Delete the second `sudo -k` child | **Confirmed with compatibility and measurement caveats.** Standard sudo documents the combined `-k`/`-l` behavior, but the latency figure is inferred rather than measured and supported sudo implementations should be stated and tested. |
| 1.3 | Overlap sudo with bridge/PAM setup | **Serialization confirmed; optimization unproven.** It adds child ownership, cancellation, PAM-ordering, and password-lifetime complexity without benchmarks or integration tests. |
| 1.4 | Move login accounting after OK | **Placement confirmed; rationale materially incorrect.** Modern glibc no longer provides the claimed ten-second alarm bound. Reordering also changes the guarantee that accounting is attempted before success is reported. |
| 2.1 | Consolidate the PAM cleanup ladder | **Confirmed with implementation caveats.** A shared epilogue is reasonable, but the sketch does not model child kill/reap state, accounting state, fd handoff, differing `pam_end` statuses, or every early password-wipe requirement. |
| 2.2 | Simplify `safe_vsnprintf` | **Confirmed.** The manual checked builtin weakens compiler-derived object-size checking. The nominal Annex K branch also uses a non-portable Microsoft-style `_TRUNCATE` signature. |
| 2.3 | Replace `/proc/self/fd` parent lookup | **Confirmed for the sole constant-path caller.** The duplicate `fstat` and path dance are real. The proposal narrows a currently generic internal function, and “no race at all” is too absolute. |
| 2.4a | Remove unused includes | **Confirmed.** The six listed includes can be removed while retaining warning-clean syntax compilation under the repository C flags. |
| 2.4b | Remove `_WIN32` guard | **Confirmed under the documented Linux-only target.** It is inconsistent with unguarded call sites and Linux-specific dependencies. |
| 2.4c | Replace `dup_pam_string` | **Confirmed.** Its behavior is equivalent to `strdup`, including allocation failure. |
| 2.4d | Delete `pipe2` fallback arms | **Confirmed under the Linux-only implementation boundary.** |
| 2.4e | Remove `read_lenstr` temporary allocation | **Confirmed.** It makes a second password copy and adds allocation failure. A direct read must wipe partial input and explicitly reject embedded NUL bytes. |
| 2.4f | Remove `MAX_PATH_LEN` | **Confirmed.** It duplicates the effective `PATH_MAX` use. |
| 2.4g | Relocate `write_all` | **Confirmed as organization only.** Declaration order must still be preserved. |
| 2.4h | Correct the JSON header comment | **Confirmed.** The request protocol is binary and length-prefixed. |
| 3.1 | Root breaks on the unprivileged path | **Real but conditional; proposed fix is incomplete.** The shipped PAM deny list rejects root before this path in the normal installation. Merely skipping the verification for UID 0 can run a root bridge while reporting an unprivileged mode. |
| 3.2 | Use canonical `PAM_USER` | **Core risk confirmed; wording overstated.** The raw name is used for `getpwnam`, but successful NSS lookup then supplies canonical `pw_name` for most downstream behavior. PAM identity must still be retrieved and validated before NSS lookup. |
| 3.3a | PAM callback function-pointer cast | **Confirmed as a formal C defect.** The CFI consequence was not tested. |
| 3.3b | `argv[0]` version comparison | **Confirmed as a harmless oddity, not a runtime bug.** A caller can deliberately set that `argv[0]`, although normal invocation does not. |
| 3.3c | Client fd 1/2 collisions | **Confirmed as latent only.** The sole production caller supplies fd 0. A general fix must account for collisions among all bootstrap, client, status, and bridge descriptors. |
| 4a | Empty passwords reach PAM | **Confirmed, conditional on host PAM policy.** Whether a blank token authenticates depends on the imported PAM stack. Rejecting it is a product-policy choice because it also excludes intentionally passwordless PAM flows. |
| 4b | Arbitrary username bytes | **Validation gap confirmed; exploit and proposed grammar overstated.** `read_lenstr` also accepts embedded NUL. util-linux `last`/`lastb` uses careful character output, while printable-ASCII-only validation may reject legitimate directory identities. |
| 4c | `execveat` fallback TOCTOU | **Mechanically confirmed; normal-path severity overstated.** LinuxIO documents kernel 5.9+, while `execveat` dates from 3.19. The path is mainly unsupported-kernel or syscall-filter compatibility code and should preferably fail closed. |
| 4d | C1 bytes in `remote_host` | **C-side gap confirmed; upstream claim overstated.** Parsed and forwarded IPs are canonicalized, but `remoteHostFromAddr` has a raw fallback. This remains low-severity defense in depth. |

## Material corrections to Account 1

### 1. Sudo polling has an omitted correctness bug

`run_cmd_as_user_with_input` checks `waitpid(..., WNOHANG)`, sleeps 100 ms, and
increments its elapsed counter. If the last sleep reaches the deadline, it does
not recheck the child before sending `SIGKILL`. A child that completed during
that final sleep can therefore be killed or misreported as a timeout.

The pidfd proposal is directionally appropriate, but a complete design must
also specify:

- a monotonic absolute deadline;
- `poll` and `waitpid` EINTR behavior;
- handling of `pidfd_open` errors other than `ENOSYS`;
- fd closure on every return path; and
- a final reap/check before timeout handling.

Reference: <https://man7.org/linux/man-pages/man2/pidfd_open.2.html>.

### 2. `sudo -k` is promising but not benchmark evidence

Standard sudo documents that `-k` used with `-l` or a command prevents use or
update of the credential cache. This supports combining `-k` with the existing
probe and removing the second process. The review's 100 ms figure remains an
inference from the polling quantum, and the claim that every denied probe
leaves a useful ticket is not established by repository evidence.

Reference: <https://manpages.debian.org/testing/sudo/sudo.8.en.html>.

### 3. The glibc accounting timeout claim is outdated

Account 1 states that the blocking wtmp lock is bounded by `alarm(10)`. Glibc's
2021 utmp/wtmp locking rewrite removed the old alarm-based scheme and uses
blocking record locks. There is no portable or current ten-second guarantee.

Moving `record_login_start` after the OK response is therefore a policy tradeoff:

- current order can delay OK but guarantees accounting is attempted first;
- reversed order improves the response path when accounting blocks, but a
  blocked or failed response can delay or skip accounting.

Reference: <https://sourceware.org/pipermail/glibc-cvs/2021q1/071990.html>.

### 4. Root behavior must be resolved as policy, not patched with a guard

The source bug is real: for target UID 0, `setuid(0)` succeeds and the
unprivileged child treats that as failed privilege dropping. In the shipped
configuration, however, `packaging/etc/linuxio/disallowed-users` contains
`root`, and the PAM account stack denies that user before bridge spawning.

The proposed `auth_user->uid != 0` guard can leave a bridge running as root while
the protocol reports `PROTO_MODE_UNPRIVILEGED`. That conflicts with
`docs/process-systemd-architecture.md`, which states that the bridge runs as
root only when the session is privileged. A future fix must explicitly choose
one of these policies:

- reject root authentication consistently;
- require root to receive privileged mode; or
- define and represent a separate root-session policy.

### 5. PAM canonical identity is a real boundary

PAM modules may change `PAM_USER`. The launcher currently performs
`getpwnam(user)` on the request value after authentication rather than reading
the final PAM item. However, once NSS lookup succeeds, `copy_auth_user` copies
`pw->pw_name`; therefore the original review is wrong that every downstream
operation uses the typed spelling.

The robust boundary is to retrieve and validate `PAM_USER` after the relevant
PAM calls, use it for NSS lookup, and define error/null/fallback behavior
explicitly. Reference:
<https://man7.org/linux/man-pages/man3/pam_get_item.3.html>.

### 6. Username validation needs a defined identity policy

The C protocol accepts arbitrary non-NUL-terminated payload bytes and then
converts them into C strings. Embedded NUL is not rejected; it silently changes
the semantic value seen by PAM and logs. Control-byte rejection is sensible,
but printable ASCII is not automatically a valid system-wide username policy
for LDAP, SSSD, AD, or Unicode-capable identity sources.

The specific statement that terminal escapes necessarily execute in `lastb` is
also not demonstrated: util-linux routes these fields through careful character
printing. Other PAM or log consumers may still behave differently, so the
general data-quality and log-safety concern remains.

Reference:
<https://kernel.googlesource.com/pub/scm/utils/util-linux/util-linux/+/refs/tags/v2.28.2/login-utils/last.c>.

### 7. The unsupported-kernel fallback should not weaken the normal invariant

The `execveat` fallback closes the validated fd and executes the resolved path,
which does restore a validation/execution race. LinuxIO's documented minimum is
Linux 5.9, while `execveat` is available from Linux 3.19. The simplest policy is
therefore to fail closed if the syscall is unavailable rather than retain a
weaker compatibility path. The hardcoded x86-64 number is only selected when
the build headers do not already define the architecture's syscall number.

## Findings missing from Account 1

### A. `close_range` errors other than `ENOSYS` leak inherited descriptors

The launcher only enters its manual close loop when `close_range` fails with
`ENOSYS`. On `EPERM`, `EINVAL`, or another error, it continues silently and can
exec the root or user bridge with unexpected descriptors at fd 6 and above.

The manual fallback also caps closure at fd 4095. The packaged systemd service
sets `LimitNOFILE=2048`, so this cap is harmless under the stock unit, but the C
helper does not itself enforce that deployment assumption.

### B. Exec-status read errors can still produce an OK response

After polling the exec-status pipe, the launcher treats a positive read as a
controlled child failure and EOF as successful exec. A negative non-EINTR read
falls through to `waitpid(..., WNOHANG)`. If the child is still present, the
parent proceeds to accounting and sends OK despite being unable to establish
the intended status-pipe result. This error path should be fail-closed.

### C. Exec-status EOF proves exec, not bridge readiness

CLOEXEC EOF proves that the child crossed a successful exec boundary. It does
not prove that the Go bridge initialized Yamux, completed bootstrap, or remained
alive. The immediate nonblocking reap reduces the race but cannot eliminate a
death immediately after the probe. A true readiness guarantee requires an
application-level acknowledgement from the bridge.

### D. Final `waitpid` errors are interpreted as successful child exit

The final wait loop retries only `EINTR`. For another error, the status variable
remains zero; the subsequent `WIFEXITED`/`WEXITSTATUS` interpretation treats that
zero as a successful exit. Supervision and logs can therefore report success
when the parent actually failed to wait for its child.

### E. Sudoers is used as an authorization oracle, not as the executor

The launcher runs `sudo -l` to obtain a Boolean policy answer. If allowed, the
already-root auth process directly sets UID/GID 0 and execs the bridge. Runtime
sudoers behavior such as execution tags, environment rules, working-directory
policy, security profiles, and command-digest intent is not necessarily applied
to that direct exec.

This may be the intended architecture—sudoers as an admin-eligibility gate—but
the source comment that sudoers is the single source of truth is broader than
the actual enforcement model and should be treated as an explicit design
decision.

## Revised priority assessment

Account 1 recommends optimizing login latency first. The independent audit does
not support that ordering because the latency figures have not been measured and
the launcher has uncovered correctness/assurance gaps.

1. Define the root, blank-password, PAM-mapped-identity, and username policies.
2. Make sudo waiting, descriptor closure, exec-status handling, and final child
   reaping fail predictably on every error path.
3. Add focused C/runtime coverage for those invariants.
4. Consolidate cleanup paths while preserving password lifetime and child/fd
   ownership.
5. Measure stage-by-stage login latency, then decide whether pidfds, merged
   `sudo -k`, concurrency, or accounting reordering are justified.
6. Apply the verified mechanical deletions and formatting-helper cleanup.

## Independent verification performed

No repository Make target was run for this read-only audit. `make check-backend`
does not exercise the C helper, while `make build-auth` and `make analyze-auth`
create binary or cache artifacts.

Two syntax-only compilations used the repository's GNU11 warning, optimization,
fortification, section, and LTO flags:

- the current `backend/auth/linuxio-auth.c`: exit 0, no diagnostics;
- the source streamed to GCC with the six proposed unused includes removed:
  exit 0, no diagnostics.

An isolated compiler probe also confirmed the §2.2 fortification distinction:
the manually supplied destination-size/object-size builtin could reduce to an
unchecked `vsnprintf`, while a normal fortified call retained checked behavior
or diagnostics. These probes created no workspace output files.

Reference: <https://gcc.gnu.org/onlinedocs/gcc/Object-Size-Checking.html>.
