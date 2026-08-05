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
| Unused includes | lines 23, 30, 35, 36, 37 | `sys/mman.h`, `strings.h`, `sys/mount.h`, `sched.h`, `ctype.h` — zero uses of any symbol. `time.h` was redundant in the reviewed baseline but is now required by the item 1.1 monotonic-deadline implementation. Keep `utmp.h` and `paths.h` — they are used. A root binary including mount/namespace headers it never uses is audit noise. |
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

# Final adjudication — independent recheck of Account 1

- **Date:** 2026-08-03
- **Original review scope:** Read-only reconciliation of Account 1 against the
  C launcher, its Go caller, PAM and systemd packaging, relevant documentation
  and history, and primary upstream behavior. The dispositions below are
  updated as fixes land.
- **Historical source baseline:** branch `dev/v0.17.0` at `688285c3`;
  `backend/auth/linuxio-auth.c` was 2,123 lines with SHA-256
  `2e7f48f3da16d7ce6894a3113a5c06092a7dedd968d2aa2f08f0ae7381ad8228`.

This adjudication records source-verified behavior and separates it from
unmeasured performance estimates and runtime assumptions. It does not claim a
live PAM/sudo authentication run or independently rely on or validate Account
1's reviewer-count claims.

## Overall conclusion

Account 1 is a useful source-review draft, but its statement that all 24
findings were confirmed is too strong. Most mechanical cleanup findings are
valid. The important corrections are:

- the sudo wait loop had a real final-boundary correctness bug; it now uses an
  event-driven, deadline-based wait, while its user-visible latency improvement
  remains unmeasured;
- the glibc accounting timeout is current, not obsolete, but is an
  implementation detail and applies per lock;
- root web logins and blank-password authentication are now explicitly rejected;
- PAM identity remapping, embedded-NUL and control handling, descriptor closure,
  child supervision, and the `execveat` fallback are corrected;
- post-fork responses retain an absolute write deadline even after the bridge
  makes the shared socket nonblocking, and the child no longer clears
  parent-owned socket timeouts;
- a failed `pam_open_session` is propagated as the final status to `pam_end`;
- CLOEXEC-pipe EOF still proves successful exec progression, not application
  readiness; a readiness guarantee remains a deliberate cross-language protocol
  change; and
- sudoers is explicitly documented as the privileged-mode authorization source,
  not as the bridge executor or a wrapper for its runtime policy.

The launcher's happy-path FD choreography and fd-based bridge validation remain
strong. All source-local correctness and hardening items in this adjudication are
implemented. The remaining items are measurement-gated performance work,
application-readiness protocol design, accounting-order policy, and dedicated
host integration coverage.

## Final disposition of Account 1

| Item | Final disposition |
|---|---|
| **1.1 — 100 ms sudo polling** | **Implemented.** The fixed-sleep loop is replaced by `pidfd_open` plus `ppoll` against an absolute monotonic deadline. Unsupported pidfd kernels use a short-sleep fallback; other pidfd errors fail closed. Both paths make a final nonblocking reap before timeout handling, close the pidfd on every path, and retry interrupted waits. The end-to-end latency improvement remains unmeasured. |
| **1.2 — merge `sudo -k` into the probe** | **Implemented.** The existing `sudo -l` probe now includes `-k`, so it ignores and does not update cached credentials. The redundant post-success invalidation child is removed. The exact latency saving remains unmeasured. |
| **1.3 — overlap sudo with PAM/bridge setup** | **Not implemented; measurement-gated.** Serialization is confirmed, but concurrency adds child ownership, cancellation, password-lifetime, and PAM-ordering complexity without evidence of a material end-to-end saving. Successful launches with complete monotonic clock reads now emit PAM, sudo, session-setup, bridge-startup, accounting, and request-to-OK timings so this decision can use the deployed path rather than historical estimates. |
| **1.4 — move accounting after OK** | **Not implemented; policy-gated.** Current glibc retains an alarm-bounded blocking lock of about 10 seconds per lock. Moving accounting after OK improves response isolation but gives up the current guarantee that accounting is attempted before success is reported. |
| **2.1 — shared cleanup epilogue** | **Implemented.** `handle_client` now has one state-aware epilogue for owned fds, child reaping, accounting, PAM session/credential teardown, `pam_end`, and password wiping. The immediate post-sudo password wipe remains in place so plaintext is not retained during bridge supervision, and intentional child fd handoffs are marked before cleanup. |
| **2.2 — simplify `safe_vsnprintf`** | **Implemented.** The dead Annex K and manual checked-builtin branches are removed. The helper now calls fortified `vsnprintf` directly, preserving compiler-derived object-size checking and C99 termination semantics. |
| **2.3 — simplify constant bridge-path lookup** | **Implemented.** The constant parent directory is opened and validated before `openat` resolves the bridge basename relative to that pinned fd. The bridge inode is then `fstat`ed once, its ownership and mode are validated, and its fd remains open for execution. This removes the validation-time `/proc/self/fd` path reconstruction without claiming metadata immutability after validation. |
| **2.4a–h — mechanical deletions and organization** | **Implemented.** Unused Linux-audit-noise includes, the ineffective platform guard, the `strdup` clone, dead `pipe2` fallbacks, the temporary field allocation, the duplicate path-size constant, and unnecessary forward declarations are removed; exact I/O helpers are colocated and the header now describes the binary request. Direct field reads wipe partial data and reject embedded NULs. `<time.h>` is retained because item 1.1 now requires it. |
| **3.1 — root unprivileged path** | **Implemented with an explicit reject-root policy.** After PAM canonicalization and NSS lookup, UID 0 receives access denied before sudo probing or bridge launch. `drop_to_user` independently rejects UID 0 so a future caller cannot create a root bridge labelled unprivileged. |
| **3.2 — canonical PAM identity** | **Implemented.** After authentication and account management, the launcher retrieves and validates `PAM_USER`, copies it out of PAM-owned storage, and uses it for NSS lookup. The resulting canonical `pw_name` continues to own sudo policy, environment, bootstrap, accounting, response, and bridge setup. |
| **3.3a–c — minor defects** | **Implemented.** The PAM callback now has the exact libpam function type without a cast; version handling examines only `argv[1]`; and client sockets occupying fd 0, 1, or 2 are parked above the fixed layout before those descriptors are rewritten, with fd 2 replaced by a non-protocol sink when necessary. |
| **4a — empty passwords** | **Implemented with an explicit reject-empty policy.** Empty wire passwords are rejected before PAM, and both authentication and account management receive `PAM_DISALLOW_NULL_AUTHTOK`. Intentionally passwordless PAM web flows are therefore out of scope. |
| **4b — username bytes** | **Implemented.** Direct field reads reject embedded NUL. Typed and PAM-canonical usernames must be valid UTF-8 and reject space, C0 controls, DEL, and C1 controls while continuing to allow non-ASCII identities. |
| **4c — `execveat` fallback** | **Implemented fail closed.** The launcher uses the architecture-provided `SYS_execveat` number and never closes the validated fd to execute a reconstructed path. An unavailable, blocked, or failed fd execution reports controlled bridge setup failure. |
| **4d — remote-host C1 bytes** | **Implemented.** Remote-host validation shares the UTF-8-aware control-codepoint filter, preserving Unicode while rejecting C0, DEL, and C1 controls. Normal canonical IP inputs are unchanged. |

References:

- [sudo(8), credential-cache behavior](https://manpages.debian.org/testing/sudo/sudo.8.en.html)
- [current glibc `utmp_file.c`](https://codebrowser.dev/glibc/glibc/login/utmp_file.c.html)
- [the unlanded `glibc/azanella/y2038` locking rewrite](https://sourceware.org/pipermail/glibc-cvs/2021q1/071990.html)
- [util-linux `last.c`](https://github.com/util-linux/util-linux/blob/master/login-utils/last.c)
  and [`carefulputc.h`](https://github.com/util-linux/util-linux/blob/master/include/carefulputc.h)
- [`pam_get_item(3)`](https://man7.org/linux/man-pages/man3/pam_get_item.3.html)
- [`execveat(2)`](https://man7.org/linux/man-pages/man2/execveat.2.html)

## Additional findings retained from the later audits

### A. Descriptor closure must fail predictably

The child only enters its manual fd-closing loop when `close_range` fails with
`ENOSYS`. Any other failure skips closure and can preserve unexpected
descriptors at fd 6 and above. The stock unit's `LimitNOFILE=2048` makes the
manual loop's 4096 cap sufficient for the packaged service, and the launcher's
own descriptors are normally CLOEXEC, so stock risk is low. PAM, NSS, policy
interposition, or future/undocumented syscall behavior prevents proving the path
unreachable. Fall back on any `close_range` failure and handle fallback setup
failure explicitly.

**Disposition: Implemented.** Any `close_range` failure now enters the fallback.
The fallback derives its bound from `RLIMIT_NOFILE` (or `_SC_OPEN_MAX` for an
infinite limit), rejects an unrepresentable bound, and treats setup or unexpected
close failures as controlled child-startup failure rather than continuing with
unknown descriptors.

Reference: [`close_range(2)`](https://man7.org/linux/man-pages/man2/close_range.2.html).

### B. Exec-status errors must fail closed

A positive pipe read reports controlled child failure and EOF reports that all
write ends closed. A negative non-EINTR read currently falls through to the
startup probe. A blocking anonymous-pipe read after readiness has few realistic
non-EINTR failures, but the branch should still fail closed.

The adjacent `waitpid(child, ..., WNOHANG)` probe also proceeds toward OK after
a non-EINTR wait error. Handle both errors through the same child-kill/reap and
PAM/fd cleanup path.

**Disposition: Implemented.** Negative exec-status reads and nonblocking wait
errors now return bridge-start errors and use the shared child/PAM/fd cleanup
path. In that startup wait-error branch, `ECHILD` is treated as already
non-waitable so cleanup cannot signal a reused PID.

### C. Exec-status EOF is not bridge readiness

CLOEXEC EOF establishes that the child crossed a successful exec boundary or
otherwise closed the status fd. It does not prove that the Go bridge read its
bootstrap, initialized Yamux, or remained alive after the point-in-time
nonblocking reap. A readiness guarantee requires a bridge acknowledgement and
is a deliberate protocol change, not a local pipe patch.

**Disposition: Deferred by design.** A correct acknowledgement requires a
dedicated inherited status channel (or framed replacement), synchronous Go-side
Yamux creation before ACK, timeout/EOF handling in C, and cross-language
integration tests. The current response continues to mean successful exec
progression, not application readiness.

### D. Final child-wait errors can report false success

The final blocking wait retries only `EINTR`. After another error, the zeroed
status is interpreted as normal exit status 0. The trigger is unlikely in the
current program, but systemd's startup-time signal reset alone does not exclude
later signal-state changes by loaded PAM or NSS code. Capture the wait result
and fail closed before interpreting the status.

**Disposition: Implemented.** The final wait result must equal the owned child
before its status is decoded. Errors are logged and return failure; `ECHILD`
avoids signalling a potentially reused PID, while other errors retain the child
for shared kill/reap cleanup.

### E. Sudoers authorizes but does not execute the bridge

The launcher consumes `sudo -l -- BRIDGE_PATH` as a Boolean. On success, the
already-root auth process sets ids and directly executes the bridge; sudo is not
in that execution chain. Runtime sudo controls such as NOEXEC, environment
rules, working-directory settings, and security profiles therefore do not wrap
the bridge process. A `Digest_Spec` still affects command matching during the
probe.

This can be a coherent login-time authorization architecture, but repository
source and history do not establish that on-demand escalation was explicitly
evaluated and rejected. Describe sudoers as the source of authorization for
privileged login mode, not as the executor or as a broader source of runtime
policy.

**Disposition: Documented architecture.** The launcher source now states that
the sudo probe is authorization only and that the already-root launcher executes
the bridge directly. No claim is made that sudo runtime tags wrap the bridge.

Reference: [`sudoers(5)`](https://man7.org/linux/man-pages/man5/sudoers.5.html).

### F. The bridge child must not clear parent response timeouts

With `Accept=yes`, `StandardInput=socket`, and `StandardOutput=inherit`, the
worker's fd 0 and fd 1 refer to the accepted socket. Forking and duplicating it
to bridge fd 3 does not create an independent socket: the child and parent still
share socket options. Clearing `SO_RCVTIMEO` and `SO_SNDTIMEO` on fd 3 therefore
also removed the parent's 10-second response timeout before the parent sent its
post-spawn error or OK response.

The adjacent Go conversion through `net.FileConn` duplicates fd 3 and sets the
shared open file description nonblocking. That makes the inherited socket
timeouts unnecessary for Go's netpoller, but it also means the parent's final
write can race with `O_NONBLOCK` and receive `EAGAIN`.

**Disposition: Implemented.** The child no longer changes the shared socket
timeouts. At the terminal response phase, the parent explicitly makes the
socket nonblocking and writes the complete framed response against one absolute
monotonic 10-second deadline, using `ppoll(POLLOUT)` for backpressure. A failed
OK write terminates and reaps the bridge through the shared cleanup path instead
of leaving an unusable session alive.

### G. `pam_open_session` failure must reach `pam_end`

The shared cleanup epilogue initializes `pam_end_status` to `PAM_SUCCESS` and
updates it on authentication, account, identity, and credential failures. The
`pam_open_session` failure branch was the exception: it returned an error to the
client but reached `pam_end` with success, so PAM data cleanup callbacks could
observe the wrong final transaction result.

**Disposition: Implemented.** The session-open failure branch now stores its PAM
return code in `pam_end_status` before entering shared cleanup. A hermetic test
cannot validate module cleanup semantics without a controllable PAM stack; that
case remains part of the root host-integration requirement below.

## Future end-to-end login performance work

A 2026-08-05 host-journal comparison found no evidence that the C hardening
slowed login. Across 20 successful pre-install sessions, systemd worker start to
`bridge spawned` had a 351 ms median. The first two sessions after installing
the hardened helper were 216 ms and 180 ms. The sample is too small for a final
benchmark, but it points away from further C micro-optimization as the first
priority.

The same journal sequence showed that client-visible work continues after the
C helper responds:

| Observed boundary | Earlier median (20 sessions) | Two post-install sessions |
|---|---:|---:|
| auth worker start to bridge spawned | 351 ms | 216 ms, 180 ms |
| bridge spawned to backend auth success | 211 ms | 266 ms, 180 ms |
| backend auth success to WebSocket connected | 450 ms | 645 ms, 347 ms |
| auth worker start to WebSocket connected | 1,087 ms | 1,126 ms, 707 ms |

This observational comparison mixes cold and warm conditions and is a
prioritization signal, not a controlled benchmark. `WebSocket connected` is
still not equivalent to the first authenticated UI
paint: the frontend may subsequently wait for configuration loading and route
readiness. Future work should measure that final boundary explicitly.

Priorities, in order:

1. **Measure complete login phases.** Correlate `LINUXIO_AUTH_*_US` with the
   HTTP `/auth/login` duration, capability discovery, WebSocket readiness,
   configuration readiness, and first authenticated render. Separate cold
   socket-activated starts from warm logins and use enough samples for median
   and tail comparisons.
2. **Remove release-network latency from the privileged login path.** The
   current handler synchronously queries GitHub with a five-second client
   timeout before writing the HTTP response. Moving this to a cache, background
   refresh, or explicit authenticated endpoint requires deciding when update
   banner data may be stale or initially absent.
3. **Evaluate capability discovery separately.** The backend currently waits
   for a bridge capability RPC before completing login; the two measured scans
   took roughly 170–256 ms. Deferring or caching it changes the initial
   capability contract and needs coordinated fallback/refresh behavior.
4. **Eliminate the blank configuration gate without accepting stale writes.**
   Sign-in clears the configuration cache, while `ConfigProvider` renders
   nothing until the new mux-backed request completes or its 2.5-second
   fallback fires. A stale-while-revalidate design must distinguish displayable
   cached values from permission to persist changes.
5. **Only then reconsider C concurrency or accounting order.** Sudo/setup
   overlap is justified only if deployed stage data shows a material saving.
   Moving accounting after OK remains a product-policy choice because it gives
   up accounting-before-success.

These are recorded work items, not implemented behavior. Items 2–4 cross API,
session, or UI semantics and require explicit decisions plus their respective
backend/frontend tests; they are not safe source-local deletions.

## Recommended order

1. Add dedicated root host-integration coverage for PAM identity, sudo outcomes,
   privilege-drop fd closure, and controlled `execveat` failure. The hermetic C
   suite now covers identity validation, the PAM conversation adapter, bridge
   policy, bootstrap encoding, child timeout/reaping, and controlled child
   startup reporting.
2. Define an application-readiness acknowledgement only with a coordinated C/Go
   protocol and integration tests; keep exec progression distinct meanwhile.
3. Follow the end-to-end performance work above before adding concurrent setup
   or accounting reordering. Successful launches with complete monotonic clock
   reads emit an `auth timing` journal event with microsecond-valued
   `LINUXIO_AUTH_*_US` fields. `LINUXIO_AUTH_BRIDGE_START_US` covers successful
   PAM session-open return through exec-ready; the aggregate
   `LINUXIO_AUTH_TOTAL_US` field runs from request handling start through the
   completed OK write; query events with
   `journalctl SYSLOG_IDENTIFIER=linuxio-auth MESSAGE='auth timing' -o json`.
   Instrumentation performs monotonic reads before the response and emits the
   journal event afterward. The pidfd wait removes fixed polling but does not
   itself establish the end-to-end saving.
4. Treat accounting-before-OK as an explicit product-policy decision.

## Verification boundary

The implementation is compiled with the repository's warning-as-error auth
build and is covered by `make test-auth`, `make analyze-auth` (cppcheck, GCC
analyzer, scan-build, and clang-tidy), and `make check-backend`. The hermetic C
suite exercises input validation, PAM conversation responses, bridge metadata
policy, binary bootstrap bytes, timing conversion, bounded response writes on a
shared nonblocking socket, controlled child-failure status, exit-status mapping,
timeout termination, and reaping. It does not exercise a real PAM stack
(including session-open cleanup status), sudoers policy, privileged descriptor
setup, `execveat`, accounting, descriptor-failure injection, or the
bridge-readiness race. Those runtime claims remain unverified until dedicated
host integration coverage exists.

---

# Cockpit comparison — 2026-08-05

Eight-agent comparison of the launcher against Cockpit @ `6a8c19cf`
(2026-08-05): `src/session/` (cockpit-session), `src/ws/cockpitauth.c`, and the
Python bridge, plus a journal extraction on the deployed host. Cockpit's
`cockpit-session` is the closest production analogue: a PAM broker spawned per
connection that hands a socket to a bridge process.

## Where the comparison landed

| Dimension | Verdict |
|---|---|
| PAM flags / empty passwords | LinuxIO stricter (`PAM_DISALLOW_NULL_AUTHTOK`; Cockpit passes flags 0) — keep |
| `PAM_USER` re-fetch | Parity |
| PAM teardown / `pam_end` status | LinuxIO strictly better — Cockpit exits without `pam_end` on setup failures and hardcodes `pam_end(PAM_SUCCESS)` on success |
| Accounting mechanism | Parity — Cockpit writes utmp/wtmp/btmp/lastlog natively and **never** delegated to pam_lastlog in its history; native writers are the proven design |
| Identity-string sanitization | LinuxIO stronger — Cockpit puts raw control bytes into btmp with only `strncpy` truncation |
| Process hardening | LinuxIO already ahead — `cockpit-session` has no prctl, no rlimits, no seccomp, and its unit carries zero sandboxing directives |
| Credential refresh after session open | Gap (fixed, see below) — Cockpit does `ESTABLISH → open_session → REINITIALIZE_CRED` (session.c:339–353) |
| Child environment | Partial gap — Cockpit's child env is `pam_getenvlist()` wholesale; LinuxIO synthesizes an allowlist and never consults PAM env (open item below) |
| Readiness signal | Real gap — Cockpit gates "session usable" on the bridge's first in-band `init` frame, never on exec (spec proposed below) |

Cockpit behaviors deliberately **not** adopted: exit-without-`pam_end` failure
handling; wholesale `env = pam_getenvlist()` (too wide for a root bridge);
unvalidated strings into btmp; the `alarm(60)` self-destruct (LinuxIO already
bounds the exchange with socket deadlines); MaxStartups logic inside the C
helper (belongs in the webserver; the socket unit already carries
`MaxConnections=16`); the on-demand superuser-bridge/polkit architecture (a
different product model, not a hardening fix).

## Disposition updates

- **§1.3 sudo/setup overlap — closed: not worth it, reopen only with data.**
  The deployed journal holds **zero** `auth timing` events because
  `/usr/local/bin/linuxio-auth` (installed Aug 3) predates commit `7f10bb26`
  that added the instrumentation (`strings` on the binary shows no
  `LINUXIO_AUTH` markers; journal retention reaches Jun 23, so absence is not
  rotation). Login volume is ~1.2/day and the parallelizable segment is
  `min(sudo, session-setup)` where session-setup is likely single-digit ms.
  Reopen only if, after deploying the instrumented binary and ≥30 logins,
  `LINUXIO_AUTH_SUDO_US` p50 exceeds ~200 ms with session-setup comparably large.
- **§1.4 accounting order — recommendation: keep accounting before OK**
  *(decision requires maintainer ratification)*. Cockpit enforces the same
  invariant (records exist before ws learns of success) and places accounting
  *earlier* than LinuxIO (before bridge spawn); LinuxIO's placement after
  exec-confirmation is strictly better — no `USER_PROCESS` record for a login
  whose bridge failed. Tradeoff of keeping: an unmeasured, structurally
  sub-millisecond write cost per login buys the guarantee that every
  reported-successful login is visible to `who`/`last`. Delegating to
  pam_lastlog stays rejected (module being removed from modern distros).
- **Readiness ACK — proposed spec** *(decision required; crosses C/Go)*.
  Collapse readiness onto the existing exec-status pipe — one fd, three
  outcomes: (1) stop re-asserting CLOEXEC on the child's status fd so it
  survives exec into the Go bridge, advertised via bootstrap; (2) pre-exec
  failure unchanged — `0x01` + diagnostic, `_exit(127)`; (3) the bridge writes
  one byte `0x02` after bootstrap parse succeeds **and** Yamux is accepting on
  fd 3, then closes the fd — or `0x03` + short UTF-8 message on pre-serve fatal
  error (Cockpit's negative-ACK `init`+`problem` analog); (4) the launcher
  polls with a single absolute deadline (10 s, env-overridable, clamped):
  `0x02` → accounting → OK; `0x03` → typed error with the bridge's message; EOF
  with no byte → died-during-startup error; timeout → SIGKILL + reap. Wire
  protocol to the webserver is unchanged. This matches Cockpit's load-bearing
  principle — readiness is bytes the child writes after it is actually serving,
  and "dead before ready" is EOF on the watched object.

## Implemented from the comparison (2026-08-05)

- `pam_setcred(PAM_REINITIALIZE_CRED)` after `pam_open_session`, failure fatal
  with the real rc propagated to `pam_end` — sshd/cockpit parity for
  Kerberos/AFS-style modules.
- `XDG_RUNTIME_DIR` is now advertised only if `/run/user/<uid>` exists and is a
  directory (pam_systemd creates it during session open; previously the path
  was synthesized blind).
- Removed the dead `PR_SET_NO_NEW_PRIVS` fallback define (never wired; the only
  prctl call is `PR_SET_DUMPABLE`). Wiring NNP on the bridge child remains a
  product question — it must never precede the sudo probe, and could break
  future setuid-helper use by the unprivileged bridge.

Verified with `make build-auth` (also `WERROR=1`), `make test-auth` (all pass),
and a clean `make analyze-auth` (cppcheck, GCC analyzer, scan-build, clang-tidy).

## New open items from the comparison

1. **Allowlisted PAM environment merge** (S–M, source-local): consult
   `pam_getenvlist()` after session open and merge an allowlist
   (`XDG_RUNTIME_DIR`, `KRB5CCNAME`, locale vars) instead of today's fully
   synthetic env — but not Cockpit's wholesale adoption.
2. **Per-source throttling of unauthenticated auth attempts** (webserver
   layer): the socket unit's `MaxConnections=16` caps concurrent instances, but
   nothing rate-limits repeated failed logins per client; Cockpit's
   `MaxStartups` analog belongs in the Go login handler.
3. **Failed-login feedback** ("N failed attempts since last success" from btmp,
   Cockpit-style) — deferred; crosses protocol + frontend, UX value only.
4. **Operational:** reinstall the instrumented helper so `LINUXIO_AUTH_*_US`
   data can accumulate (the §1.3/§1.4 gates depend on it). The journal also
   shows 47 session-opens vs 24 session-closes since Jun 23 — baseline this
   during host-integration work (item 5 below).
5. **Host-integration plan (refines the earlier recommendation):** tier 1,
   hermetic per-PR via `pam_wrapper` + `pam_matrix` with a checked-in test
   service file — PAM sequencing incl. session-close-exactly-once on every
   post-open failure path, `PAM_USER` canonicalization via an aliasing module,
   fd-layout/privilege-drop assertions from a stub bridge that dumps
   `/proc/self/fd` + `getresuid`, an execveat failure matrix (missing,
   non-executable, setuid-rejected, exits-post-exec, garbage-on-fd-3), and
   accounting record contents against tmpfile paths. Tier 2, disposable root
   host — sudoers outcome matrix (NOPASSWD / password / absent, with faillock),
   real `who`/`last`/`lastb` assertions, the bridge-readiness race regression
   (only meaningful once the ACK ships), and kill/cleanup ordering against the
   journal as oracle. Cockpit itself has zero unit tests for its session C code
   (VM tier only) — pam_wrapper is borrowed from samba/sssd practice instead.
