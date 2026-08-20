# Production Diagnostic Data Policy

This policy applies to panic and `SIGQUIT` tracebacks, journald records, pprof
profiles, copied or downloaded logs, future support bundles, and core dumps.
Diagnostic output is retained and shared more broadly than live process memory,
so authentication material must never be used as diagnostic identity.

## Prohibited data

The following values must not appear in diagnostic labels, log messages or
fields, errors returned to clients, profiles, or support artifacts:

- authentication session IDs and cookie values;
- passwords, PAM conversation data, authorization headers, and other bearer
  tokens;
- internal owner keys that contain any prohibited value; and
- arbitrary request values used as labels before validation.

The authentication `session_id` is a bearer credential, not a harmless
identifier. Exact-field suppression of `SESSION_ID` in the journald handler is
defense in depth only; production code must not pass the credential to the
logger under any field name or embed it inside `MESSAGE` or `ERROR`.

## Permitted correlation data

Goroutine labels may contain only identifiers whose source has been validated:

- `component`: a static LinuxIO component name;
- `route`: the canonical registered route name, never the client request value
  before route lookup;
- `task_id` and `task_type`: LinuxIO-created Task identity and registered type;
- `stream_id`: the bridge-local stream counter;
- `session_ref`: the non-secret diagnostic reference described below; and
- `uid`: the authenticated numeric UID.

Usernames are intentionally excluded from traceback and pprof labels. They may
remain in access-controlled structured audit logs where user attribution is
the purpose of the record.

`session_ref` is `sr-` followed by the first 16 bytes of a domain-separated
SHA-256 digest of the 128-bit random session credential. It is stable across the
webserver and bridge so their records can be correlated, but it is not accepted
for authentication and must never be used as an authorization key. It is
pseudonymous and linkable across retained diagnostics, so normal diagnostic
access and retention controls still apply.

Client-visible errors may include safe operation context such as a registered
route. They must not include internal map keys, owner keys, cookies, or other
implementation identity.

## Sink-specific controls

- Panic and `SIGQUIT` output may include the permitted goroutine labels above.
  The bridge's stderr is journal-connected, so these dumps are governed by the
  same access and retention policy as journald.
- Normal production binaries do not expose the pprof HTTP server. Profiling
  builds are diagnostic artifacts, must not be deployed as production
  binaries, and must still use sanitized labels because binary CPU and
  goroutine profiles retain pprof labels.
- LinuxIO currently has no support-bundle collector. A future collector must
  apply this policy before including journal, traceback, or profile data and
  must not collect process memory by default.
- The webserver and the auth worker that launches each bridge use
  `LimitCORE=0`. This prevents routine core artifacts from retaining
  credentials held in process memory.

## Emergency traceback suppression

`GODEBUG=tracebacklabels=0` suppresses all Go labels in panic, `SIGQUIT`, and
pprof `debug=2` traceback headers. It does not remove labels from ordinary CPU
or goroutine profiles. Use it only as a temporary emergency control if an
unsafe label is discovered; after labels are sanitized it would also remove the
safe ownership information this policy preserves.

Setting `GODEBUG` only on `linuxio-auth@.service` does not reach the bridge. The
C launcher clears and reconstructs the bridge environment, so an emergency
deployment must set or preserve the forced value in the child environment after
that `clearenv()` boundary.

## Regression coverage

Backend tests enforce the credential boundary at the main known escape points:

- goroutine tracebacks contain `session_ref`, component, and UID but not the
  cookie, `session_id`, or username labels;
- unknown client-controlled routes cannot become goroutine labels;
- owner rate-limit errors and serialized result frames do not contain the
  session credential;
- Yamux lookup errors do not contain the session credential; and
- direct `SESSION_ID` journald fields remain suppressed as defense in depth.

