# Go 1.27 Findings and Adoption Strategy

## Status

Reviewed on 2026-08-20 against the official
[Go 1.27 release notes](https://go.dev/doc/go1.27), the
[Go 1.27 release history](https://go.dev/doc/devel/release#go1.27.0), and the
current LinuxIO source tree. Go 1.27.0 was released on 2026-08-19. LinuxIO
already declares `go 1.27.0` in `backend/go.mod`, and the repository Make and
CI paths derive their toolchain version from that declaration.

This document is the decision record and dependency-ordered roadmap for Go
1.27 adoption. It distinguishes:

- behavior already present in LinuxIO;
- automatic runtime or standard-library benefits that require no rewrite;
- targeted improvements worth implementing;
- compatibility and security risks that need explicit handling; and
- release-note changes with no current LinuxIO use case.

The conclusions are source-verified unless a section explicitly calls for a
benchmark or runtime observation. The expected allocation and JSON performance
gains have not been measured on LinuxIO workloads.

## Executive Decision

Keep Go 1.27 and adopt its features selectively. Do not start a repository-wide
generics rewrite merely because methods can now declare type parameters.

The release is already valuable to LinuxIO through:

- the v2-backed `encoding/json` implementation and LinuxIO's selective strict
  `encoding/json/v2` request boundaries;
- the generally available goroutine-leak profile;
- size-specialized allocation for small objects;
- the standard `uuid` package;
- HTTP response-body draining and the default header-value limit;
- `stdversion` checking during `go test`; and
- language support for promoted field selectors in struct literals.

The highest-priority follow-up is not generics. It is diagnostic privacy:
Go 1.27 now includes `runtime/pprof` goroutine labels in panic and SIGQUIT
tracebacks, while LinuxIO currently labels goroutines with raw session IDs and
usernames. A session ID is also the authentication cookie value. That exposure
must be removed or explicitly disabled before traceback labels are treated as
safe production diagnostics.

Generic methods have two credible LinuxIO applications:

1. a small typed D-Bus API owned by `SystemSession`; and
2. a larger route-schema DSL cleanup that binds metadata and progress methods
   to `Route[Request, Result]`.

Only the D-Bus change is suitable as an incremental adoption. The route-schema
work should wait for a planned schema redesign because it changes many route
declarations and validation timing.

## Current LinuxIO Baseline

### Toolchain and targets

- `backend/go.mod` pins final Go 1.27.0.
- `Makefile` derives the managed Go toolchain version from `go.mod`.
- CI and CodeQL use `backend/go.mod` as their Go version source.
- The backend intentionally targets Linux, primarily amd64 and arm64.
- Darwin, Plan 9, and big-endian ppc64 release changes do not affect the
  supported product boundary.
- Staticcheck is enabled in `backend/.golangci.yml` after the Go 1.27 tooling
  compatibility issue was resolved.
- `gci` still has a temporary section for Go 1.27's root-level `uuid` package;
  remove it once `gci` classifies that package correctly.

### Go 1.27 features already adopted

- Strict JSON v2 decoding is used for normal bridge requests, login requests,
  and indexer configuration:
  - `backend/common/ipc/bridge/request_decoder.go`
  - `backend/webserver/auth/auth.go`
  - `backend/bridge/handlers/indexer/config.go`
- The standard `uuid` package validates durable-operation IDs and formats
  libvirt UUIDs.
- `strings.CutLast` is used by the D-Bus signal parser.
- Promoted embedded fields are initialized directly in network backend and
  file-progress struct literals.
- Typed atomics, `sync.WaitGroup.Go`, and `slices.Backward` are already in use;
  no remaining `atomictypes`, `slicesbackward`, or unsafe-modernization target
  was found.
- `runtime/pprof` labels annotate bridge sessions, streams, routes, and Tasks.
- Debug builds expose `/debug/pprof/goroutineleak` through the existing
  loopback-only `pprofdebug` server and `make build-leak-profile` workflow.
- The container reverse proxy uses `httputil.ReverseProxy.Rewrite`, not the
  deprecated `Director` field, and regenerates trusted forwarding headers with
  `ProxyRequest.SetXForwarded`.

### Existing generic design

LinuxIO already uses generic functions and generic types where the types are
known at compile time. Important examples include:

- `apischema.Route[Request, Result]` and its typed handler wrappers;
- `dbusclient.GetProperty[T]`;
- `JSONRequestDecoder[T]`;
- `fetchHistory[S, P]`;
- `hwSnapshotCache[T]`;
- `cloneJSON[T]`;
- `transferResult[T]`; and
- generator/test helpers such as `sortedKeys[T]` and `decodeChannelJSON[T]`.

There are no current methods declaring their own type parameters and no
generic-function value assignments or conversions that benefit materially
from Go 1.27's broader inference rules.

## Language Findings

### Generic methods

Go 1.27 permits a concrete method to declare type parameters in addition to
any type parameters belonging to its receiver. This places a generic operation
inside a type's namespace instead of requiring a package-level function.

Important constraints remain:

- interface methods cannot declare type parameters;
- a generic concrete method cannot implement an interface method;
- a method normally needs explicit type arguments when its type parameter
  appears only in its result type;
- uninstantiated generic methods are not generally available through
  reflection; and
- language support can arrive before linters, formatters, IDEs, and generators
  understand every new construct.

See the accepted
[generic-method proposal](https://github.com/golang/go/issues/77273) and the
[Go method specification](https://go.dev/ref/spec#Method_declarations).

#### Candidate 1: typed D-Bus session methods

`backend/bridge/internal/dbusclient/properties.go` currently declares:

```go
func GetProperty[T any](
    ctx context.Context,
    obj godbus.BusObject,
    iface string,
    property string,
) (T, error)
```

Approximately fourteen production callers pass `session.Context()` and a
D-Bus object explicitly. `SystemSession` already owns the context, connection,
and object access, making it a natural receiver:

```go
func (s SystemSession) GetProperty[T any](
    obj godbus.BusObject,
    iface string,
    property string,
) (T, error)
```

Expected benefit:

- removes repeated context plumbing;
- prevents using an unrelated context for a session operation;
- keeps typed D-Bus conversion in the D-Bus client's namespace; and
- permits incremental call-site migration without changing heterogeneous
  registry boundaries.

A related method could handle single-result calls:

```go
func (s SystemSession) CallResult[T any](method string, args ...any) (T, error)
```

Keep `CallStore` for calls with multiple results. Arbitrary result tuples are
not a good generics target.

Decision: adopt `SystemSession.GetProperty[T]` only when the D-Bus client is
next being changed. Consider `CallResult[T]` separately after inventorying
which `CallStore` sites truly have one result. Do not turn this into a broad
D-Bus abstraction project.

#### Candidate 2: route-bound fluent schema methods

The route schema already binds `Request` and `Result` through
`Route[Request, Result]`, but these package functions independently erase their
types into `RouteSpecOption`:

- `WithTaskProgress[Detail]`;
- `WithTaskMetadata[Request]`; and
- `WithTaskIdentity[Request]`.

The metadata and identity wrappers perform runtime assertions after type
erasure. A route-bound API could instead make mismatched request callbacks a
compile-time error:

```go
route.
    WithTaskProgress[PullProgress]().
    WithTaskMetadata(func(req PullRequest) bridgeipc.TaskMetadata { /* ... */ }).
    Run(handler)
```

Potential benefit:

- progress configuration lives with the typed route;
- metadata and identity callbacks inherit the route request type;
- runtime request assertions can be removed; and
- declarations become easier to read when several Task options are combined.

Cost and risk:

- roughly twenty progress declarations and a similar number of metadata or
  identity declarations would change;
- constructor validation currently happens while options are applied and must
  retain the same failure timing;
- Task lifetime, privilege, retry, and generated-contract ownership must remain
  unchanged; and
- the API generator and source guards must be traced through the redesign.

Decision: defer. Reconsider only as a deliberate `apischema` DSL cleanup, then
validate the combined contract with `make generate` and `make test-quiet`.

#### Lower-value generic-method candidates

| Candidate | Assessment | Decision |
|-----------|------------|----------|
| `durabletask.Record.DecodeResult[T]` | Could centralize repeated typed decoding of opaque result JSON. Route-specific state and error behavior would still remain at each caller. | Reconsider only if more durable operations create meaningful duplication. |
| Typed Task data-attacher registration | Could move one unavoidable type assertion to registration time. The registry remains heterogeneous and currently has very few registrations. | Defer. |
| `UseSessionValue[T]` callback helper | Could remove outer result variables from D-Bus session callbacks. It adds another callback abstraction and may obscure synchronous control flow. | Prefer direct session methods first. |
| Generic optional-scalar helper | Four similar helpers could become `OptionalNonZero[T comparable]`, but this was possible before Go 1.27 and type-specific names may be clearer. | Opportunistic cleanup only. |

#### Poor fits for generics

Do not introduce generics for:

- heterogeneous Call, Channel, Task, or data-attacher registries after their
  typed registration boundary;
- dynamic YAML, Compose, D-Bus variant, journal, or unknown JSON values;
- reflection-based API schema traversal;
- interfaces such as configuration, NTP, or libvirt backends that would need
  generic interface methods;
- broad `Map`, `Filter`, or iterator helpers over domain-specific projections;
  or
- existing helpers such as `fetchHistory`, `cloneJSON`, `sortedKeys`, and
  `transferResult`, which have no natural receiver needing an additional type
  parameter.

### Promoted selectors in struct literals

Go 1.27 permits any valid field selector as a keyed struct-literal key,
including a promoted field through an embedded struct. LinuxIO already uses
this in network backends and file-progress values.

Benefit: less nested literal boilerplate.

Risk: this syntax initially caused Staticcheck to be disabled because the
analyzer could not understand it. Staticcheck is now enabled again, but the
episode is a reminder to verify the complete lint toolchain before adopting a
new syntax pervasively.

Decision: retain the existing literals. Do not rewrite unrelated literals just
to use the syntax.

### Generalized generic-function inference

Type arguments can now be inferred when assigning or converting a generic
function to a matching non-generic function type in all assignment contexts.

No LinuxIO call site currently needs this. Existing generic functions are
called directly or inferred through ordinary function arguments.

Decision: no action. Use the inference naturally when a real function-value
site appears; do not restructure code to demonstrate it.

## Toolchain and Command Findings

| Go 1.27 change | LinuxIO impact | Strategy |
|----------------|----------------|----------|
| GCC-compatible `@response-file` parsing in `compile`, `link`, `asm`, `cgo`, `cover`, and `pack` | Existing Make invocations do not approach problematic command-line lengths and do not need external build-system interoperability. | No action. |
| Removal of Bazaar (`bzr`) module fetching | No Bazaar-hosted dependency was found. | No action. |
| Removed `GODEBUG` values accepted only at their final default | No `godebug` directive, `//go:debug` comment, or obsolete behavior selection was found. | Keep avoiding compatibility switches unless a measured regression requires one. |
| `go test` runs `stdversion` by default | Protects a Go 1.27 module from accidentally using newer standard-library symbols when a later toolchain is used. | Automatic benefit; keep the module directive accurate. |
| `go test -json` adds optional `OutputType` | Current quiet Make logging does not consume structured test JSON. | No action until a log consumer needs it. |
| `go doc package@version` | Makes dependency-version inspection easier. | Developer convenience only. |
| `go doc -ex` and improved example printing | Makes executable examples easier to inspect. | Developer convenience only. |
| New `go fix` modernizers: `atomictypes`, `embedlit`, `slicesbackward`, `unsafefuncs` | Existing code already uses typed atomics and `slices.Backward`; promoted literals are adopted; no unsafe migration target was found. | Existing Make modernization is sufficient. |
| Removal of `fmtappendf` analyzer | No local dependency on that analyzer. | No action. |
| Rename of `waitgroup` analyzer to `waitgroupgo` | No conflicting explicit configuration found. | No action. |
| `go mod tidy` merges duplicate `require` blocks | `backend/go.mod` already has the direct/indirect two-block form. | Automatic hygiene through Make. |
| `go tool trace -http=:PORT` binds to localhost | Safer local trace viewing. | Automatic developer-tool benefit. |

## Runtime, Compiler, and Linker Findings

### Traceback labels: useful diagnostics with a credential risk

For modules declaring Go 1.27 or later, panic and SIGQUIT tracebacks include
goroutine labels in each goroutine header. The durable opt-out is
`GODEBUG=tracebacklabels=0`.

LinuxIO attaches the following labels to long-lived goroutines:

- raw `session_id`;
- username in `user`;
- `stream_id`;
- route;
- Task ID and Task type; and
- component names.

The session ID is also written as the secure, HTTP-only authentication cookie
and used to look up the authenticated session. Therefore a production
traceback can disclose an active bearer credential to journald, crash capture,
support bundles, or anyone with access to process diagnostics. The existing
structured logs also contain raw session IDs in several paths, so the Go 1.27
change broadens an existing sensitive-data handling problem rather than
creating the only exposure.

Required strategy:

1. Define a non-secret diagnostic identity separate from the authentication
   token. Prefer a dedicated random correlation ID or a keyed digest. Do not
   label goroutines with the cookie value.
2. Decide whether usernames are permitted in production tracebacks and support
   bundles. Replace them with numeric UID or another approved identifier if
   necessary.
3. Add a regression test that obtains a goroutine dump and proves the raw
   session token is absent while the safe correlation fields remain useful.
4. Audit structured session logging under the same policy; otherwise removing
   the label closes only one disclosure path.
5. Until the labels are sanitized, configure
   `GODEBUG=tracebacklabels=0` in the production service environment if panic
   and SIGQUIT output is not sufficiently access-controlled.

### Faster small allocations

The compiler now calls size-specialized allocation routines for some objects
smaller than 80 bytes. Go reports up to 30% lower allocation cost for affected
allocations and an expected overall improvement of about 1% in
allocation-heavy programs, at a cost of approximately 60 KB per binary.

LinuxIO's JSON, HTTP, IPC, routing, and Task paths allocate many small objects,
so an automatic gain is plausible. Four Go executables mean the binary-size
increase applies more than once, but it remains small relative to the shipped
binaries.

Decision: keep the default. Do not set
`GOEXPERIMENT=nosizespecializedmalloc` without a measured regression. Add a
Make-owned benchmark target before making LinuxIO-specific performance claims.

### Goroutine-leak profile

The `goroutineleak` profile is generally available in `runtime/pprof` and at
`/debug/pprof/goroutineleak`; the old `goroutineleakprofile` experiment is
removed. The profile detects a useful but incomplete class of goroutines
blocked on unreachable synchronization primitives.

LinuxIO already exposes it only in `pprofdebug` builds on loopback ports.

Decision: retain the existing debug-only workflow. Keep documentation clear
that the profile cannot find leaks whose synchronization primitive remains
reachable from globals or runnable goroutines.

### Other runtime and compiler changes

| Change | LinuxIO assessment | Decision |
|--------|--------------------|----------|
| Permanent removal of `asynctimerchan`; time channels are always synchronous | No compatibility switch or buffered timer-channel assumption was found. Synchronous timer channels have already been the default for several releases. | No action. |
| Relative `//line` and `/*line*/` paths resolve from the containing source file | No such directive was found. | No action. |
| Simpler closure symbol names and code sharing across inlining | No test asserts closure symbols and no code compares function code pointers. Journald source attribution can change cosmetically. | Accept the implementation change. |
| macOS linker `-macos` and `-macsdk` options | LinuxIO does not ship Darwin binaries. | No action. |

## Standard-Library Findings

### `encoding/json/v2` and `encoding/json/jsontext`

Go 1.27 makes `encoding/json/v2` and `encoding/json/jsontext` available without
an experiment. The v2 API provides option-bearing marshal/unmarshal entry
points and lower-level token/value processing. Its defaults reject invalid
UTF-8 and duplicate JSON object member names.

The original `encoding/json` API is now backed by the v2 implementation while
preserving v1 semantics. Exact error text may differ, marshal performance is
approximately unchanged, and unmarshal performance is significantly faster.
The temporary build fallback is `GOEXPERIMENT=nojsonv2`.

LinuxIO already uses strict v2 decoding at trusted contract boundaries:

- every normal bridge route uses one `JSONRequestDecoder[T]` with
  `RejectUnknownMembers(true)`;
- login decoding rejects unknown fields; and
- indexer configuration patches reject unknown fields.

Tests cover unknown and case-mismatched fields, duplicate names, invalid UTF-8,
trailing JSON, and typed semantic/syntactic errors. No test was found that
depends on the exact legacy v1 error string.

Strategy:

- keep strict v2 decoding for LinuxIO-owned request envelopes;
- let remaining v1 imports receive the faster backing implementation
  automatically;
- do not blanket-convert upstream status, distro configuration, Docker data,
  or other externally owned payloads where tolerance provides forward
  compatibility;
- review nil-versus-empty collections, `omitempty`, map ordering, duration
  encoding, merge behavior, HTML escaping, and `null` behavior before
  migrating a wire contract; and
- use `GOEXPERIMENT=nojsonv2` only as a short-lived compatibility escape hatch
  tied to a filed upstream issue and a regression test.

### Standard `uuid`

The new root `uuid` package parses and generates UUIDs. LinuxIO already uses it
for canonical durable-operation ID validation and libvirt UUID formatting.
There is no external UUID dependency to remove.

Session tokens must remain cryptographically random opaque credentials rather
than being converted to UUIDs merely because the package exists.

Decision: current adoption is complete. Remove the temporary `gci` package
classification workaround when the formatter understands the standard
package.

### Post-quantum cryptography

Go 1.27 adds `crypto/mldsa`, ML-DSA key/signature support in `crypto/x509`, and
ML-DSA signature schemes in TLS 1.3. It also adds the `crypto.MLDSAMu` signaling
hash and opt-in `MLKEM1024` key exchange.

LinuxIO's locally generated certificate intentionally uses ECDSA P-256. There
is no post-quantum certificate, signing, or interoperability requirement.

Decision: no adoption. Revisit only when browser/client compatibility and a
product security requirement justify a certificate and TLS policy change.

### Experimental SIMD

The portable `simd` package and architecture-specific `simd/archsimd` package
require `GOEXPERIMENT=simd`. `archsimd` remains unstable and architecture
specific; Go 1.27 changes its amd64 API and adds arm64 Neon and WebAssembly
support.

LinuxIO is predominantly I/O and control-plane code. Its heavy compression
paths already use optimized third-party libraries, and no measured numeric hot
loop was found.

Decision: do not enable experimental SIMD. Require a profile, a bounded kernel,
cross-architecture behavior, and a fallback before reconsidering.

### Complete minor-library inventory

| Package/change | LinuxIO impact | Strategy |
|----------------|----------------|----------|
| `bytes.CutLast` | No remaining `[]byte` last-separator pattern was found. | Use naturally when such a call site appears. |
| `strings.CutLast` | Already used by the D-Bus signal parser. | Adoption complete. |
| Faster `compress/flate`; encoded bytes may change | Main archive ZIP/gzip writers use `klauspost/compress/zip` and `pgzip`, not the standard writers. `image/png` output used for QR data may still change. | Do not compare compressed bytes unless the encoding itself is the contract. |
| `crypto.MLDSAMu` | No External-mu ML-DSA signing. | No action. |
| `crypto/ecdsa.PrivateKey.Sign` validates hash length | LinuxIO generates ECDSA certificates but does not directly call this signing method with custom hash options. | No action. |
| `crypto/tls.QUICConfig.ClientHelloInfoConn` | No QUIC server. | No action. |
| TLS `MLKEM1024` | No explicit post-quantum curve policy. | No action. |
| Deprecation of `tls.Config.Rand` | No use found. | No action. |
| Explicit hybrid PQ curves can override default-disabling `GODEBUG` settings | No explicit curve preferences. | No action. |
| `tls.ConnectionState.LocalCertificate` | No connection-state requirement for the locally presented chain. | Use only if a concrete diagnostics or policy need appears. |
| Removal of `tlsunsafeekm`, `tlsrsakex`, `tls3des`, `tls10server`, and `x509keypairleaf` | No removed switch was found. | No action. |
| Broader `x509/pkix.Name` value parsing | No custom distinguished-name parser. | Automatic compatibility improvement. |
| Raw DER signature-algorithm fields on X.509 certificate, request, and revocation-list types | No low-level signature-algorithm inspection. | No action. |
| `SystemCertPool` honors `SSL_CERT_FILE`/`SSL_CERT_DIR` on Windows and Darwin | Linux behavior is unchanged. | No action. |
| `pkix.RDNSequence.String` renders unknown string-valued OIDs as strings | No output contract depends on the old DER-hex rendering. | Accept behavior change. |
| `database/sql.ConvertAssign` | `database/sql` is not used. | No action. |
| `database/sql/driver.RowsColumnScanner` | LinuxIO does not implement a SQL driver. | No action. |
| `go/constant.StringLen` | No compiler-like constant processing. | No action. |
| `go/scanner.Scanner.End` | No production scanner. | No action. |
| `go/token.File.String` | No relevant token-file formatting. | No action. |
| `go/types.Hasher` and `HasherIgnoreTags` | The API generator does not need a type-keyed hash table. | No action. |
| Permanent `gotypesalias` removal; aliases always produce `go/types.Alias` | No compatibility switch or alias-node assumption was found. | No action. |
| `hash/maphash.Hasher` and `ComparableHasher` | No custom hash table or Bloom filter implementation. | No action. |
| `math/big.Int.Divide` with rounding modes | `math/big` is used only for certificate serial values. | No action. |
| `math/rand/v2.(*Rand).N[T]` | `math/rand/v2` is unused. | No action. |
| `net.UnixConn` reads return direct `io.EOF` | LinuxIO's EOF handling is compatible and does not require a wrapped `*net.OpError`. | No action. |
| `net/http` ALPN support for user-provided connections exposing TLS state | The redirect listener already yields `*tls.Conn`, but server `NextProtos` is not configured and no custom protocol is required. | No standalone change. Treat HTTP/2 enablement as a separate tested feature. |
| HTTP/2 RFC 9218 client priorities | No material benefit while HTTP/2 is not explicitly configured on this listener. | No action. |
| Bounded automatic draining of unread HTTP/1 response bodies on `Close` | LinuxIO consistently closes bodies; early-return paths can now reuse connections more reliably. No pathological idle-client configuration was found. | Automatic benefit. |
| `http.Server.MaxHeaderValueCount` and default limit | The public server gets the default limit of 500 automatically. | Consider an explicit project constant and boundary test if LinuxIO wants a stable policy independent of future Go defaults. |
| `httptest.NewTestServer` with an in-memory fake network | Ten tests use the old loopback `httptest.NewServer`, including six indexer and four WebSocket tests. | Migrate suitable indexer tests first. WebSocket tests need client transport/dialer adaptation. |
| `url.URL.Clone` and `url.Values.Clone` | No repeated error-prone manual deep-copy site was found. | Use when a real copy is needed. |
| Child goroutines inherit `runtime/secret` mode | `runtime/secret` is unused. Secret handling would require a deliberate end-to-end design. | No action. |
| Plan 9 defines `syscall.Errno` | Plan 9 is outside the target. | No action. |
| `testing/synctest.Sleep` | Fourteen backend tests call real `time.Sleep`. Session and router timer tests are plausible fake-time candidates; Docker, D-Bus, process, journald, and WebSocket tests may depend on real systems. | Convert only deterministic in-process timing tests. |
| Unicode data advances from 15 to 17 | Can change `IsControl`, `IsPrint`, and `IsSpace` results used in filename/content checks and DNF parsing. | Accept the standards update; add a regression test only for LinuxIO-specific classifications that form a contract. |

## Port Findings

### Darwin

Go 1.27 requires macOS 13 Ventura or later. LinuxIO's backend target is Linux
and should not add a portability layer for this change.

### Big-endian Linux ppc64

The toolchain now emits ELFv2 binaries on `linux/ppc64`, requires a compatible
kernel/runtime, and adds cgo, PIE, and external-link support. LinuxIO builds
amd64 and arm64, not big-endian ppc64.

Decision: no action. If ppc64 ever becomes a product target, treat it as a new
port with real systemd, D-Bus, PAM, Docker, libvirt, and packaging validation.

## Risk Register

| Risk | Likelihood/impact | Mitigation |
|------|-------------------|------------|
| Raw session credentials in Go 1.27 tracebacks | High impact; currently source-verified | Replace credential labels with non-secret correlation IDs; test dumps; use `tracebacklabels=0` until safe. |
| Tooling lag behind new syntax/packages | Medium; already occurred with Staticcheck and `gci` | Keep language adoption incremental, run the complete Make lint target, remove workarounds when upstream support lands. |
| JSON v2 wire-semantic drift after blanket migration | Medium if migration is indiscriminate | Keep strict v2 at owned boundaries; review each external contract and migration difference. |
| Exact JSON error-text drift in v1-backed implementation | Low | Assert structured types/codes or stable substrings, not complete standard-library messages. |
| Compressed output bytes differ | Low | Test decoded content; use byte goldens only where exact encoding is intentional. |
| Unicode classification changes | Low | Preserve only LinuxIO-specific classifications with explicit tests. |
| Closure symbol names change | Low | Do not make behavior depend on compiler-generated names. |
| Small binary-size growth from specialized allocation | Low | Measure packaged binaries; disable only for a demonstrated regression. |
| Experimental SIMD API churn | Avoidable | Do not enable without a measured kernel and fallback. |

## Adoption Strategy

### Phase 0: Protect production diagnostics

- [ ] Define which identifiers may appear in panic, SIGQUIT, journald, support
  bundle, and pprof output.
- [ ] Replace raw `session_id` goroutine labels with a non-secret correlation
  identifier.
- [ ] Decide whether `user` should remain a username, become numeric UID, or be
  removed from tracebacks.
- [ ] Add a traceback regression test proving an authentication cookie value
  cannot appear in goroutine headers.
- [ ] Audit raw session IDs in structured logs under the same policy.
- [ ] Set `GODEBUG=tracebacklabels=0` in production until the preceding work is
  complete if diagnostic access and retention are not already sufficiently
  restricted.

Exit criteria:

- an active session credential never appears in a production traceback;
- safe labels still identify component, route, Task, and stream ownership; and
- the diagnostic policy is tested and documented.

### Phase 1: Finish release hygiene

- [x] Pin final Go 1.27.0 in `backend/go.mod`.
- [x] Make local and CI toolchain selection derive from `go.mod`.
- [x] Re-enable Staticcheck after its Go 1.27 syntax support landed.
- [x] Resolve the resulting Go 1.27 Staticcheck findings, including the
  app-update failure contract, error-string casing, and reverse-proxy
  `Director` deprecation.
- [ ] Remove the temporary `gci` `prefix(uuid)` workaround once upstream
  recognizes the Go 1.27 standard package.
- [ ] Change `README.md` from "Go 1.27 RC2" to "Go 1.27".
- [ ] Keep the Go 1.27 upgrade in the normal dependency-update and security
  patch cadence.

Exit criteria:

- `make check-backend-quiet` passes with Staticcheck enabled;
- no release-candidate wording remains; and
- no workaround remains after its upstream compatibility issue is fixed.

### Phase 2: Use the new deterministic test facilities

Start with tests where the new API removes a real external dependency:

1. Convert suitable indexer HTTP tests from loopback
   `httptest.NewServer` to in-memory `httptest.NewTestServer`.
2. Preserve the returned test client's transport; do not accidentally send the
   request through `http.DefaultClient`.
3. Convert pure session/router timer tests to `testing/synctest` and
   `synctest.Sleep` where all relevant goroutines and time sources remain
   inside the bubble.
4. Leave real WebSocket, Docker, D-Bus, journald, subprocess, and systemd tests
   on real time/network primitives unless a focused adapter establishes a
   faithful boundary.

Exit criteria:

- migrated tests use no loopback port or real sleep;
- handler panics fail the owning test directly;
- timing assertions are deterministic under repeated execution; and
- `make check-backend-quiet` passes.

### Phase 3: Pilot one generic method at a natural ownership boundary

When the D-Bus client is next modified:

1. Add `SystemSession.GetProperty[T]` with the session's context propagated
   internally.
2. Preserve useful operation context and error identity.
3. Test successful conversion, D-Bus failure, wrong variant type, and canceled
   session context.
4. Migrate a small cohesive caller group first, preferably the repeated
   systemd unit-property wrappers.
5. Inspect the full diff before migrating other packages. Stop if the method
   only moves syntax without simplifying ownership.
6. Retain the package-level helper temporarily only if a real non-session
   caller still needs it; otherwise remove it after all callers migrate.

Do not combine this pilot with the route-schema redesign.

Exit criteria:

- the receiver owns every implicit dependency used by the method;
- call sites are materially simpler;
- interface and reflection boundaries are unchanged; and
- `make check-backend-quiet` passes with the complete lint toolchain.

### Phase 4: Make policy and performance explicit only where useful

- [ ] Decide whether LinuxIO wants to pin `MaxHeaderValueCount` explicitly or
  intentionally follow Go's default. Add a boundary test if pinning it.
- [ ] Add a repository Make benchmark target before measuring JSON, IPC, HTTP,
  Task, or allocation performance. Never bypass Make with direct Go tooling.
- [ ] Compare Go 1.26 and 1.27 only on representative payloads and service
  workloads; separate compiler allocation gains from JSON decoder gains.
- [ ] Record executable/package size changes for the four Go binaries.
- [ ] Keep `GOEXPERIMENT=nojsonv2` and
  `GOEXPERIMENT=nosizespecializedmalloc` as emergency diagnostic controls, not
  standard build configuration.

Exit criteria:

- performance claims have reproducible Make-owned evidence;
- any explicit HTTP limit has a documented product reason; and
- no compatibility experiment remains enabled without an issue, owner, test,
  and removal condition.

### Phase 5: Reconsider the route DSL only with a separate design

The fluent generic route API is optional. Before implementation, a design must
show that it:

- removes runtime request assertions;
- preserves option validation timing;
- preserves generated route types and policies;
- does not add a parallel registration API;
- maintains cancellation and invalidation ownership; and
- produces a net simplification across representative Call, Task, and Channel
  declarations.

Because this crosses schema declarations and generated contracts, use
`make generate` followed by `make test-quiet`. Do not combine it with unrelated
route migrations.

## Explicit Non-Goals

- No repository-wide conversion of generic functions into methods.
- No generic collection utility package.
- No generic interface-method workaround through reflection or type erasure.
- No blanket JSON v2 API migration.
- No adoption of experimental SIMD without profiling evidence.
- No post-quantum certificate change without a product requirement and client
  interoperability plan.
- No Darwin, Plan 9, or ppc64 portability layer.
- No dependency on exact compiler closure names, JSON error strings, or
  compressed bytes.
- No direct invocation of underlying Go lint, format, generation, or test
  tools; verification remains Make-owned.

## Verification Matrix

| Change type | Required final verification |
|-------------|-----------------------------|
| Backend-only Go 1.27 adoption | `make check-backend-quiet` |
| Go-owned API contract, schema, or generator input | `make generate`, then `make test-quiet` |
| Frontend and backend behavior together | `make test-quiet` |
| Claim about real browser navigation or chunk behavior | `make test-frontend-browser-quiet` in addition to the relevant source checks |
| Runtime performance claim | A repository Make benchmark target with recorded workload, environment, repetitions, and comparison baseline |
| Host/systemd/Docker/libvirt behavior | The appropriate explicit integration target and environment; unit tests alone do not establish runtime behavior |

Quiet-target failure logs live under `.cache/test-logs/` and must be inspected
before rerunning with normal output.

## Related Go 1.27 GODEBUG History

The [Go GODEBUG history](https://go.dev/doc/godebug#go-127) records two Go 1.27
entries that are not highlighted as standalone items in the main release-note
body because they were also backported:

- `htmlmetacontenturlescape` controls URL escaping in HTML meta refresh
  content; and
- `fips140ems=0` can disable EMS enforcement under FIPS 140-3.

No matching LinuxIO override or dependency was found. The same history records
the Go 1.27 defaults for `tracebacklabels` and the Darwin/Windows
`x509sslcertoverrideplatform` behavior; only traceback-label privacy is
material to the current Linux target.

## References

- [Go 1.27 release notes](https://go.dev/doc/go1.27)
- [Go 1.27.0 release history](https://go.dev/doc/devel/release#go1.27.0)
- [Go language specification](https://go.dev/ref/spec)
- [Generic-method proposal](https://github.com/golang/go/issues/77273)
- [Generic-function inference proposal](https://github.com/golang/go/issues/77245)
- [`encoding/json` v2 migration notes](https://pkg.go.dev/encoding/json#hdr-Migrating_to_v2)
- [`net/http.Server`](https://pkg.go.dev/net/http#Server)
- [`net/http/httptest.NewTestServer`](https://pkg.go.dev/net/http/httptest#NewTestServer)
- [`runtime/pprof`](https://pkg.go.dev/runtime/pprof)
- [Go GODEBUG history](https://go.dev/doc/godebug#go-127)
