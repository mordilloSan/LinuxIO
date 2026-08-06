# LinuxIO authentication launcher review

**Current scope:** `backend/auth/linuxio-auth.c`, its Go peers, packaged systemd
units, and the review/test evidence recorded below.

This document describes the launcher as it exists now: implemented behavior,
current evidence, and remaining work. It does not preserve superseded findings
or source-line snapshots.

## Current assessment

**Date:** 2026-08-06
**Scope:** source review of the C launcher, its Go peers, packaged systemd
units, and the current test evidence.

This assessment separates source-verified behavior from runtime observations.
It does not claim a live PAM/sudo/host integration run where that coverage is
not available.

## Overall conclusion

The launcher is structurally sound on its security boundaries. Current source
review found no memory-safety defect, authentication bypass,
privilege-escalation path, or live C-to-Go READY/GO mismatch. Its important
properties are:

- the sudo wait loop had a real final-boundary correctness bug; it now uses an
  event-driven, deadline-based wait, while its user-visible latency improvement
  is not established by controlled measurement;
- the glibc accounting timeout is current, not obsolete, but is an
  implementation detail and applies per lock;
- root web logins and blank-password authentication are now explicitly rejected;
- PAM identity remapping, embedded-NUL and control handling, descriptor closure,
  child supervision, and the `execveat` fallback are corrected;
- post-fork responses retain an absolute write deadline even after the bridge
  makes the shared socket nonblocking, and the child no longer clears
  parent-owned socket timeouts;
- a failed `pam_open_session` is propagated as the final status to `pam_end`;
- the launcher and bridge now use a two-phase READY/GO handoff: READY reports
  completion of pre-Yamux initialization, then the launcher records accounting,
  writes the complete OK response, and sends GO before Yamux may start;
- request framing, authentication, and startup work share a 20-second deadline
  from request receipt, targeting a 10-second response margin inside the
  webserver's 30-second deadline; synchronous PAM calls are checked afterward
  rather than forcibly interrupted; and
- sudoers is explicitly documented as the privileged-mode authorization source,
  not as the bridge executor or a wrapper for its runtime policy.

The launcher's happy-path FD choreography and fd-based bridge validation remain
strong. The implemented source-local correctness and hardening work is recorded
below. Two lifecycle items remain open: graceful launcher handling of service
SIGTERM and bounded TERM-to-KILL escalation when reaping a nonconforming bridge.
Remaining work also includes accounting-order policy and dedicated cross-language
and host-integration coverage of the startup-handoff protocol.

## Current implemented dispositions

| Item | Final disposition |
|---|---|
| **Sudo wait** | **Implemented.** The fixed-sleep loop is replaced by `pidfd_open` plus `ppoll` against an absolute monotonic deadline. Unsupported pidfd kernels use a short-sleep fallback; other pidfd errors fail closed. Both paths make a final nonblocking reap before timeout handling, close the pidfd on every path, and retry interrupted waits. One deployed observation shows no fixed polling delay, but does not establish the end-to-end improvement. |
| **Sudo policy query** | **Implemented.** The launcher no longer sends a password to sudo or uses its credential cache: after PAM authenticates once, root runs `sudo -n -l -U <user> -u root -- BRIDGE_PATH` solely to query sudoers. |
| **Concurrent policy query** | **Not implemented.** The root-side policy query is approximately 11 ms on the measured host, leaving too little work to justify concurrent child ownership and cancellation. Reconsider only if deployed timing shows the policy query becoming material again. |
| **Accounting before success response** | **Policy-gated.** Current glibc retains an alarm-bounded blocking lock of about 10 seconds per lock. Moving accounting after OK improves response isolation but gives up the current guarantee that accounting is attempted before success is reported. |
| **Shared cleanup epilogue** | **Implemented.** `handle_client` now has one state-aware epilogue for owned fds, child reaping, accounting, PAM session/credential teardown, `pam_end`, and password wiping. The password is wiped immediately after PAM finishes and before the policy query because no later stage needs it; intentional child fd handoffs are marked before cleanup. |
| **Formatting helper** | **Implemented.** The dead Annex K and manual checked-builtin branches are removed. The helper now calls fortified `vsnprintf` directly, preserving compiler-derived object-size checking and C99 termination semantics. |
| **Bridge-path lookup** | **Implemented.** The constant parent directory is opened and validated before `openat` resolves the bridge basename relative to that pinned fd. The bridge inode is then `fstat`ed once, its ownership and mode are validated, and its fd remains open for execution. This removes the validation-time `/proc/self/fd` path reconstruction without claiming metadata immutability after validation. |
| **Mechanical cleanup and I/O** | **Implemented.** Unused Linux-audit-noise includes, the ineffective platform guard, the `strdup` clone, dead `pipe2` fallbacks, the temporary field allocation, the duplicate path-size constant, and unnecessary forward declarations are removed; exact I/O helpers are colocated and the header now describes the binary request. Direct field reads share the absolute request deadline, wipe partial data, and reject embedded NULs. `<time.h>` is retained for monotonic-deadline support. |
| **Root session policy** | **Implemented with an explicit reject-root policy.** After PAM canonicalization and NSS lookup, UID 0 receives access denied before sudo probing or bridge launch. `drop_to_user` independently rejects UID 0 so a future caller cannot create a root bridge labelled unprivileged. |
| **Canonical PAM identity** | **Implemented.** After authentication and account management, the launcher retrieves and validates `PAM_USER`, copies it out of PAM-owned storage, and uses it for NSS lookup. The resulting canonical `pw_name` continues to own sudo policy, environment, bootstrap, accounting, response, and bridge setup. |
| **Descriptor and argument defects** | **Implemented.** The PAM callback now has the exact libpam function type without a cast; version handling examines only `argv[1]`; and client sockets occupying fd 0, 1, or 2 are parked above the fixed layout before those descriptors are rewritten. When fd 2 held the client, its `/dev/null` replacement is explicitly left open across exec so bridge stderr remains valid. |
| **Empty-password policy** | **Implemented with an explicit reject-empty policy.** Empty wire passwords are rejected before PAM, and both authentication and account management receive `PAM_DISALLOW_NULL_AUTHTOK`. Intentionally passwordless PAM web flows are therefore out of scope. |
| **Username validation** | **Implemented.** Direct field reads reject embedded NUL. Typed and PAM-canonical usernames must be valid UTF-8 and reject space, C0 controls, DEL, and C1 controls while continuing to allow non-ASCII identities. |
| **`execveat` fallback** | **Implemented fail closed.** The launcher uses the architecture-provided `SYS_execveat` number and never closes the validated fd to execute a reconstructed path. An unavailable, blocked, or failed fd execution reports controlled bridge setup failure. |
| **Remote-host control bytes** | **Implemented.** Remote-host validation shares the UTF-8-aware control-codepoint filter, preserving Unicode while rejecting C0, DEL, and C1 controls. Normal canonical IP inputs are unchanged. |

## Supplemental fixes — 2026-08-06 post-performance re-review

A fresh security, correctness, lifecycle, and C-to-Go protocol review found no
memory-safety defect, authentication bypass, privilege-escalation path, or live
READY/GO mismatch introduced by the performance work. It confirmed four small
launcher defects that were then fixed and two lifecycle hardening items that
remain open.

| Finding | Disposition | Hermetic coverage and boundary |
|---|---|---|
| **Absolute deadline omitted from request reads** | **Implemented.** The header and all four length-prefixed fields now use `ppoll` plus the same 20-second monotonic `request_deadline_ns`; a peer cannot renew a 30-second per-read timeout indefinitely by trickling bytes. | The C suite covers valid, embedded-NUL, truncated, and partial-field deadline cases. It does not yet drive a deliberately trickled full request through `handle_client`. |
| **NULL error omitted the required length prefix** | **Implemented.** Every error response now writes a length-prefixed string; `NULL` is encoded as length zero, matching Go's unconditional decoder. | An exact-byte C test covers the ten-byte empty-error frame. Cross-language response exchange remains open. |
| **fd 2 `/dev/null` replacement retained `FD_CLOEXEC`** | **Implemented.** `replace_stderr_with_devnull` leaves the replacement stderr inheritable before bridge exec. | A forked helper test verifies fd 2 has no `FD_CLOEXEC`; the real spawn/exec fd-2 layout remains part of host integration. |
| **Sudo-probe infrastructure failures were silent** | **Implemented.** Parent-observed wait/timeout failures preserve errno and receive an infrastructure diagnostic; child setup/exec status 127 receives a distinct diagnostic. Both still fail closed to unprivileged mode, while ordinary policy denial remains non-error control flow. | Existing tests cover argv construction and timeout errno/reaping. They do not capture journald or exercise a real sudoers policy. |
| **Service SIGTERM skips the launcher epilogue** | **Open.** The launcher has no SIGTERM handoff while blocked in the session-long child wait, so service stop can skip `record_login_end`, PAM session close, credential deletion, and `pam_end`. | Requires signal-safe forwarding/state handling plus a service-stop cleanup test. |
| **SIGTERM cleanup has no bounded escalation** | **Open.** `terminate_and_reap_child` sends the selected signal once and then blocks in `waitpid`; a bridge that ignores SIGTERM can pin the worker. | Add a grace deadline, SIGKILL escalation, reap, and a stubborn-child regression test. |
| **`MaxConnections=16`** | **Capacity characteristic, not a defect.** An accepted auth worker supervises its bridge for the full session, so the socket setting caps live sessions as well as slow handshakes. | Capacity and per-source throttling remain deployment/webserver concerns, not C framing changes. |

The review also rechecked and declined the fixed-width utmp NUL, deliberate
Unicode username, and inherited nonblocking/socket-timeout reports as defects.
The remaining comments about stale paths, magic constants, and logging style are
cosmetic.

References:

- [sudo(8), credential-cache behavior](https://manpages.debian.org/testing/sudo/sudo.8.en.html)
- [current glibc `utmp_file.c`](https://codebrowser.dev/glibc/glibc/login/utmp_file.c.html)
- [the unlanded `glibc/azanella/y2038` locking rewrite](https://sourceware.org/pipermail/glibc-cvs/2021q1/071990.html)
- [util-linux `last.c`](https://github.com/util-linux/util-linux/blob/master/login-utils/last.c)
  and [`carefulputc.h`](https://github.com/util-linux/util-linux/blob/master/include/carefulputc.h)
- [`pam_get_item(3)`](https://man7.org/linux/man-pages/man3/pam_get_item.3.html)
- [`execveat(2)`](https://man7.org/linux/man-pages/man2/execveat.2.html)

## Current hardening properties

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

### B. Startup-status errors fail closed

Negative startup-status reads and nonblocking wait errors return bridge-start
errors and use the shared child/PAM/fd cleanup path. The fd 4 transport is a
bidirectional socketpair for READY/GO. In the startup wait-error branch,
`ECHILD` is treated as already non-waitable so cleanup cannot signal a reused
PID.

### C. READY/GO establishes pre-Yamux readiness

The launcher advertises the handshake in the
bootstrap and preserves one endpoint of a bidirectional socketpair at fd 4
across exec. The Go bridge completes initialization up to, but not including,
Yamux creation; it then writes READY and blocks waiting for GO. A fatal error
before READY produces a negative status plus a bounded diagnostic. After READY,
the launcher records accounting and writes the complete authentication OK
response before sending GO. Only after receiving valid GO may the bridge close
fd 4 and create Yamux on fd 3.

This ordering is a transport-ownership invariant: before GO, only the launcher
may write fd 3, so eagerly emitted Yamux control bytes cannot precede, overlap,
or corrupt the authentication response. A failed OK write never releases the
bridge. READY therefore proves progress beyond exec and completion of pre-Yamux
initialization, not that Yamux is already serving; a post-GO Yamux creation
failure appears as a transport/session failure. The C and Go sides have isolated
tests, but no test yet launches the real pair and verifies fd inheritance,
READY/GO ordering, response integrity, and failure outcomes across the language
boundary.

### D. Final child-wait errors fail closed

The final wait result must equal the owned child before its status is decoded.
Errors are logged and return failure; `ECHILD` avoids signalling a potentially
reused PID, while other errors retain the child for shared kill/reap cleanup.

### E. Sudoers authorizes but does not execute the bridge

After PAM authenticates and canonicalizes the account, the root launcher runs
`sudo -n -l -U <user> -u root -- BRIDGE_PATH` and consumes its exit status as a
Boolean. Exit status zero grants privileged bridge mode; every denial, error,
signal, or timeout selects the unprivileged bridge. The query has no password
input and cannot prompt. This makes the contract explicit: PAM authenticates
the login once, while sudoers authorizes whether that user's bridge may remain
root.

On success, the already-root auth process sets ids and directly executes the
bridge; sudo is not in that execution chain. Runtime sudo controls such as
NOEXEC, environment rules, working-directory settings, and security profiles
therefore do not wrap the bridge process. A `Digest_Spec` still affects command
matching during the probe. Hosts that disable root's use of sudo or install a
policy plugin without compatible `-U` list semantics fail closed to
unprivileged mode. Sudo-specific password or MFA stacks are intentionally not a
second login authentication layer.

This is the selected login-time authorization architecture: sudoers is the
source of authorization for privileged mode, not the bridge executor or a
broader source of runtime policy. Removing sudo's second authentication stack
is intentional; installations that require sudo-specific MFA must account for
that distinction when enabling privileged LinuxIO access.

**Disposition: Implemented and documented architecture.** The launcher now
performs the authorization-only query from its root context, with stdin and
stdout isolated from the client connection, and directly executes the bridge.
Parent-observed query infrastructure failures and child setup/exec failure are
now journalled distinctly from normal policy denial. No claim is made that sudo
runtime tags wrap the bridge.

Reference: [`sudoers(5)`](https://man7.org/linux/man-pages/man5/sudoers.5.html).

### F. Parent responses remain bounded on the shared socket

With `Accept=yes`, `StandardInput=socket`, and `StandardOutput=inherit`, the
worker's fd 0 and fd 1 refer to the accepted socket. The bridge's fd 3 shares
socket state with the parent, and Go makes its open file description nonblocking.
The child does not change the shared socket timeouts. At the terminal response
phase, the parent explicitly makes the
socket nonblocking and writes the complete framed response against one absolute
monotonic 10-second deadline, using `ppoll(POLLOUT)` for backpressure. A failed
OK write terminates and reaps the bridge through the shared cleanup path instead
of leaving an unusable session alive.

### G. `pam_open_session` failure reaches `pam_end`

The session-open failure branch stores its PAM return code in `pam_end_status`
before entering shared cleanup. A hermetic test cannot validate module cleanup
semantics without a controllable PAM stack; that case remains part of the root
host-integration requirement below.

## End-to-end login performance record

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
still not equivalent to the first authenticated UI paint: the frontend may
subsequently wait for configuration loading and route readiness.

**Decision (2026-08-06): the login performance implementation work is closed.**
This is a maintainer scope decision, not a claim that the small deployed sample
establishes a stable distribution. The ≥30 comparable-login gate remains the
evidence threshold for quoting medians or tails, or for reopening C concurrency
and accounting-order work.

Current status:

1. **Release-network latency is removed from login.** `Login` no longer queries
   GitHub. Privileged sessions request the separate authenticated
   `GET /api/update-info` endpoint; that request still performs a synchronous
   GitHub lookup with a five-second client timeout, but it cannot delay the
   authentication response.
2. **Capability discovery is decoupled and instrumented.** `AuthContext`
   requests it after the authenticated mux opens, ignores stale completions, and
   may reuse cached values on reload. The stage measured about **14.5 ms warm**
   and **51.6 ms on the first login after restart**; those are stage observations,
   not proof of a stable end-to-end improvement.
3. **The blank configuration gate is separate UI work.** Sign-in clears the
   configuration cache, while `ConfigProvider` renders nothing until the new
   mux-backed request completes or its 2.5-second fallback fires. Any
   stale-while-revalidate design must distinguish displayable cached values from
   permission to persist changes.
4. **Further measurement is optional evidence work.** A future benchmark should
   correlate `LINUXIO_AUTH_*_US`, HTTP `/auth/login`, capability discovery,
   WebSocket readiness, configuration readiness, and first authenticated render,
   with cold and warm samples reported separately.

No sudo/setup overlap or accounting reorder is planned without new data and an
explicit accounting-policy decision.

## Recommended order

1. Implement graceful launcher SIGTERM handling and bounded TERM-to-KILL child
   cleanup together, then cover normal and signal-ignoring bridges. These share
   child ownership and epilogue semantics and should not become independent
   signal paths.
2. Add cross-language framing and startup coverage: feed Go's real auth request
   into the C parser, decode C success/error responses with Go, decode C
   bootstrap with Go, and launch the real C/Go pair through READY, GO, negative
   status, EOF, timeout, inherited-fd behavior, and response ordering. Add
   `handle_client` malformed-request/error-result cases alongside it.
3. Add dedicated root host-integration coverage for PAM identity and sequencing,
   sudoers outcomes, privilege-drop fd closure, controlled `execveat` failure,
   accounting, and shutdown cleanup. Keep this distinct from the isolated C and
   Go protocol tests.
4. Add the smaller Go gaps: direct `Authenticate` coverage and the bridge command
   wrapper's rejection of empty bootstrap session/user fields.
5. Treat accounting-before-OK as an explicit product-policy decision. Only
   reopen login-path performance changes after the measurement gate produces
   evidence of a material remaining stage.

Successful launches with complete monotonic clock reads emit an `auth timing`
journal event with microsecond-valued `LINUXIO_AUTH_*_US` fields.
`LINUXIO_AUTH_BRIDGE_START_US` covers successful PAM session-open return through
the bridge's pre-Yamux READY acknowledgement; it does not include post-GO Yamux
creation. `LINUXIO_AUTH_TOTAL_US` runs from request handling start through the
successful GO release after the completed OK write. Query events with
`journalctl SYSLOG_IDENTIFIER=linuxio-auth MESSAGE='auth timing' -o json`.

## Verification boundary

The implementation is compiled with the repository's warning-as-error auth
build and is covered by `make test-auth`, `make analyze-auth` (cppcheck, GCC
analyzer, scan-build, and clang-tidy), and `make check-backend`. The hermetic C
suite exercises deadline-aware and ambiguous input, PAM conversation responses,
bridge metadata policy, exact sudo policy-query arguments, binary bootstrap
bytes, empty-error frame bytes, fd-2 CLOEXEC state, timing conversion, bounded
response writes on a shared nonblocking socket, controlled child-failure status,
exit-status mapping, timeout termination, and reaping. It does not exercise a
full trickled request through `handle_client`, journald assertions, a real PAM
stack (including session-open cleanup status), sudoers policy, the complete
privileged descriptor/exec layout, `execveat`, accounting, descriptor-failure
injection, service-stop cleanup, a SIGTERM-ignoring bridge, or a real
cross-language READY/GO exchange with authentication-response ordering. Those
runtime claims remain unverified until dedicated integration coverage exists.

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
| Readiness signal | Partial parity through an implemented startup handoff — LinuxIO's bridge reports pre-Yamux READY, waits for the launcher's GO, and only then creates Yamux. Unlike Cockpit's first in-band `init` frame, READY does not prove the transport is already serving. Real cross-language integration coverage remains open. |

Cockpit behaviors deliberately **not** adopted: exit-without-`pam_end` failure
handling; wholesale `env = pam_getenvlist()` (too wide for a root bridge);
unvalidated strings into btmp; the `alarm(60)` self-destruct (LinuxIO already
bounds the exchange with socket deadlines); MaxStartups logic inside the C
helper (belongs in the webserver; the socket unit already carries
`MaxConnections=16`); the on-demand superuser-bridge/polkit architecture (a
different product model, not a hardening fix).

## Disposition updates

- **Concurrent sudo/setup work — closed by maintainer decision; do not reopen
  without data.** The deployed smoke observations put the root-side policy query
  around 9–16 ms and session setup below 1 ms. They are not a stable
  distribution, but show too little overlap ceiling to justify concurrent child
  ownership. Reopen only after at least 30 comparable successful logins show
  `LINUXIO_AUTH_SUDO_US` p50 above roughly 200 ms with session setup comparably
  large.
- **Accounting order — recommendation: keep accounting before OK**
  *(decision requires maintainer ratification)*. Cockpit enforces the same
  invariant (records exist before ws learns of success) and places accounting
  *earlier* than LinuxIO (before bridge spawn). LinuxIO records after pre-Yamux
  READY, avoiding `USER_PROCESS` records for exec and pre-READY failures, but a
  post-GO Yamux creation failure can still leave a short-lived record. Tradeoff
  of keeping: a structurally potentially blocking write buys the guarantee that
  every reported-successful login is visible to `who`/`last`. The single 0.2 ms
  observation below is preliminary and does not characterize lock contention or
  tail latency. Delegating to pam_lastlog stays rejected (module being removed
  from modern distros).
- **READY/GO startup handoff — implemented; cross-language verification remains
  open.** The child inherits one endpoint of a bidirectional startup socketpair
  at fd 4, as advertised by the bootstrap. Pre-exec failure is `0x01` plus a
  diagnostic followed by `_exit(127)`. After bootstrap parsing and non-Yamux
  initialization, the bridge writes READY (`0x02`) and blocks reading fd 4. A
  fatal pre-READY error is `0x03` plus a short message (Cockpit's negative-ACK
  `init`+`problem` analog). Request framing reads, authentication, and startup
  work share a 20-second absolute
  deadline measured from request receipt, targeting a 10-second response margin
  inside the webserver's 30-second read deadline. The configured bridge-ready
  phase defaults to 10 seconds, is clamped to 1–20 seconds, and is clipped to the
  remaining request budget; the sudo child wait is clipped to the same budget.
  The launcher does not interrupt synchronous PAM calls, so expiry during PAM is
  detected after the call returns and prevents bridge launch. Startup outcomes
  are: READY → accounting
  → complete OK → GO (`0x04`) → Yamux creation; `0x03` → typed error with the
  bridge's message; EOF with no byte → died-during-startup error; timeout →
  SIGKILL + reap. EOF or a byte other than GO while the bridge waits also fails
  closed.

  The ordering prevents protocol corruption on shared fd 3: the bridge cannot
  create Yamux, and therefore cannot emit Yamux control bytes, until the launcher
  has completed the authentication response and transferred ownership with GO.
  If the OK write fails, no GO is sent and the bridge is terminated. READY is
  consequently a pre-Yamux rendezvous, not proof that the bridge is already
  serving. The webserver auth wire protocol remains unchanged.

### Preliminary deployed measurement (2026-08-05 15:25, instrumented helper, cold boot)

One sample, first login after fresh install + reboot: total request-to-OK
**30.4 ms** — sudo probe 16.9 ms, PAM 11.4 ms, bridge start 1.2 ms, session
setup 0.8 ms, accounting **0.2 ms** (phases sum to total within 0.1 ms).
Within this observation, the concurrent-work overlap ceiling was
`min(sudo, session-setup)` = **0.8 ms**, sudo was ~12× under the 200 ms reopen
threshold, and accounting was 0.2 ms (0.6% of helper time). One uncontended
sample neither closes the ≥30-login measurement gate nor characterizes
accounting tail latency. It confirms that fixed 100–200 ms polling was absent
from this run, but it is not a matched validation of the sudo-wait or
policy-query latency improvement.

The same trace places request start to WebSocket connection at about **616 ms**,
making the helper about **5%** of that observed path. The remainder occurred
after the helper in this trace. That trace predates capability decoupling; the
separate response-to-WebSocket interval (roughly 32–40 ms in the newer stage
timing) remains opaque and should be instrumented independently.

### Post-fix login-path breakdown (2026-08-05 16:58–17:05)

Two successful logins using the corrected READY/GO binaries provide the first
post-fix smoke baseline. The earlier 16:50 invalid-response-magic failure is
excluded. The center below is the midpoint of exactly two observations, not a
stable median or percentile; the ≥30 comparable-login measurement gate remains
open. This baseline predates the capability-discovery decoupling described
below, so its capability scan row is historical and must not be read as the
current login contract.

| Order | Step | Observed duration | Two-sample center | Share of centered 605 ms path |
|---:|---|---:|---:|---:|
| 1 | PAM authentication | 8.5–9.7 ms | **9.1 ms** | 1.5% |
| 2 | sudo policy check | 11.7–15.6 ms | **13.6 ms** | 2.2% |
| 3 | PAM session setup | 0.73–0.75 ms | **0.74 ms** | 0.1% |
| 4 | Bridge initialization → READY | 12.3–12.5 ms | **12.4 ms** | 2.0% |
| 5 | Native login accounting | 0.067–0.071 ms | **0.069 ms** | <0.1% |
| 6 | Auth response, GO, and minor launcher gaps | 0.066–0.067 ms | **0.067 ms** | <0.1% |
|  | **C authentication helper total** | **34.7–37.2 ms** | **36.0 ms** | **5.9%** |
| 7 | GO → Yamux started | 0.058–0.062 ms | **0.060 ms** | <0.1% |
| 8 | Yamux started → session created | 0.014–0.266 ms | **0.140 ms** | <0.1% |
| 9 | Session created → capability scan begins | 0.238–0.471 ms | **0.355 ms** | <0.1% |
| 10 | Capability scan | 190.1–197.4 ms | **193.8 ms** | **32.0%** |
| 11 | Capabilities complete → authentication succeeds | 0.341–0.534 ms | **0.438 ms** | <0.1% |
|  | **Request → authentication succeeded** | **225.7–235.7 ms** | **230.7 ms** | **38.1% cumulative** |
| 12 | Authentication succeeded → WebSocket connected | 371.6–377.2 ms | **374.4 ms** | **61.9%** |
|  | **Request → WebSocket connected** | **597.3–612.9 ms** | **605.1 ms** | **100%** |

The two visible bottlenecks in this pre-decoupling sample are the
authentication-success-to-WebSocket window
(center **374.4 ms**) and the capability scan (center **193.8 ms**); together
they account for about **94%** of the centered login path. At the time of this
comparison, the journal could derive the aggregate capability duration from its
start/end messages but could not split it into detector durations. The
performance follow-up now emits one `capabilities timing` event after each
successful scan. `LINUXIO_CAPABILITIES_US` covers detector launch through all
detectors completing, and the 16 `LINUXIO_CAPABILITIES_<NAME>_US` fields report
each detector's wall time. Query them with
`journalctl SYSLOG_IDENTIFIER=linuxio-bridge MESSAGE='capabilities timing' -o json`.
Accumulate at least 30 comparable warm-login samples, with cold starts reported
separately, before quoting a stable improvement.

The first four deployed timing events identified `memory_inventory` as the
scan's critical path: **133.8–141.5 ms** of a **134.3–144.3 ms** total. A
10-command warm host comparison measured `udevadm info --export-db` at
158.3 ms median versus 6.9 ms for a targeted property query. The availability
probe now queries `/sys/class/dmi/id` directly and retains the `dmidecode`
fallback; the actual memory-module fetch still exports the full udev database.
The command comparison supports the change, but its deployed login-path effect
remains to be measured.

## Implemented follow-ups (2026-08-05 to 2026-08-06)

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
- Replaced the authenticated-user `sudo -k -S` probe with a root-side,
  non-interactive `sudo -n -l -U <user> -u root -- BRIDGE_PATH` query. PAM now
  authenticates once and sudoers supplies only the privileged-mode policy
  decision; the password is never sent to sudo. Pre-change calm-login samples
  placed this phase around 83–95 ms. Two deployed post-change logins measured
  the policy query at **9.4 ms** and **10.2 ms**; that is smoke evidence, not a
  stable performance distribution.
- Decoupled capability discovery from bridge/login completion. `StartBridge` no
  longer runs or persists `system.get_capabilities` synchronously, and login
  JSON carries no capability fields. After the authenticated mux opens,
  `AuthContext` invokes the existing RPC once per frontend authentication
  bootstrap asynchronously, ignores stale completions, and persists valid
  results. Reloads may use cached values; a new sign-in clears the prior cache.
  The measured capability stage is about **14.5 ms warm** and **51.6 ms on the
  first login after restart**. These stage timings do not establish an overall
  login improvement before new deployed samples are collected.
- Moved release checking out of `Login`. Privileged sessions now request the
  authenticated `GET /api/update-info` endpoint after login, so its synchronous
  five-second GitHub client timeout cannot delay the authentication response.
- Applied the post-performance transport hardening summarized above: absolute
  request-read deadlines, mandatory empty error length prefixes, inheritable
  replacement stderr for the fd-2 layout, and distinct fail-closed sudo-probe
  infrastructure diagnostics.

The latest launcher changes were verified with `make check-backend` (including
the warning-as-error C tests, Go race tests, lint, and dead-code scan) and a clean
`make analyze-auth` (cppcheck, GCC analyzer, scan-build, and clang-tidy).

## New open items from the comparison

1. **Allowlisted PAM environment merge** (S–M, source-local): consult
   `pam_getenvlist()` after session open and merge an allowlist
   (`XDG_RUNTIME_DIR`, `KRB5CCNAME`, locale vars) instead of today's fully
   synthetic env — but not Cockpit's wholesale adoption.
2. **Per-source throttling of unauthenticated auth attempts** (webserver
   layer): the Go login semaphore caps concurrent HTTP authentication attempts
   at 8 per webserver process, while the socket unit's `MaxConnections=16` caps
   accepted auth workers and therefore live supervised bridge sessions. Neither
   rate-limits repeated failed logins per source; Cockpit's `MaxStartups` analog
   belongs in the Go login handler.
3. **Failed-login feedback** ("N failed attempts since last success" from btmp,
   Cockpit-style) — deferred; crosses protocol + frontend, UX value only.
4. **Optional measurement evidence:** accumulate at least 30 comparable
   successful-login `LINUXIO_AUTH_*_US` samples before quoting a stable
  improvement, revisiting the closed concurrent-work decision, or using accounting
   observations to characterize tail behavior.
5. **Host-integration plan (refines the earlier recommendation):** tier 1,
   hermetic per-PR via `pam_wrapper` + `pam_matrix` with a checked-in test
   service file — PAM sequencing incl. session-close-exactly-once on every
   post-open failure path, `PAM_USER` canonicalization via an aliasing module,
   fd-layout/privilege-drop assertions from a stub bridge that dumps
   `/proc/self/fd` + `getresuid`, an execveat failure matrix (missing,
   non-executable, setuid-rejected, exits-post-exec, garbage-on-fd-3), and
   accounting record contents against tmpfile paths. Tier 2, disposable root
   host — root-side sudoers outcome matrix (NOPASSWD / password / absent,
   unrelated command, and `root_sudo` disabled), including confirmation that
   the command-specific `-U` query never prompts and that incompatible policy
   plugins fail closed,
   real `who`/`last`/`lastb` assertions, the implemented READY/GO handoff across
   the real C/Go process boundary, authentication-response ordering, its startup
   race regressions, and kill/cleanup ordering against the journal as oracle.
   Cockpit itself has zero unit tests
   for its session C code (VM tier only) — pam_wrapper is borrowed from
   samba/sssd practice instead.
6. **Graceful service-stop cleanup:** install signal-safe launcher SIGTERM
   handling that forwards shutdown to the owned bridge and allows native login
   accounting plus PAM session/credential teardown to run through the shared
   epilogue.
7. **Bounded bridge termination:** replace the unbounded SIGTERM + blocking wait
   path with a grace deadline, SIGKILL escalation, and guaranteed reap. Cover a
   child that deliberately ignores SIGTERM.
