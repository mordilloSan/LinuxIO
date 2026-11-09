---
name: new-bridge-route
description: Checklist for adding or changing a linuxio-bridge route (binding, privilege, errors, logging, generated contract, tests, docs). Use when adding a route under backend/bridge/handlers or when asked to add an API endpoint.
---

Work through these in order. Each line names the doc section that owns the detail; read that section instead of guessing.

1. **Shape.** Pick `Call`, `Mutation`, `MutationVoid` or `MutationTask` per `docs/api-contract.md` → "Route Modes And Kinds" and "Backend Handler Shapes". Declare it in `backend/bridge/handlers/<domain>/handlers.go` per `docs/bridge_handler_patterns.md` → "`handlers.go` Layout" and "Route Namespaces".
2. **Privilege.** If the handler needs root — writes under `/etc` or `/var/lib`, mounts, block-device or filesystem ioctls, systemd, NetworkManager, Docker, libvirt, PackageKit, user/group changes, power — declare `apischema.Privileged()` in the binding, never a check inside the handler. Every Docker write is privileged: the `docker` group is empty by policy. Reads stay unprivileged unless they expose sensitive data. `docs/privilege_pattern.md`.
3. **Retry safety.** A `Call` that is safe to re-run declares `RetrySafe()`. `docs/bridge_handler_patterns.md` → "Call Retry Safety".
4. **Validation.** Validate at the boundary. Client errors wrap a sentinel with `%w`: `ErrInvalidArgs` 400, `ErrForbidden` 403, `ErrNotFound` 404, `ErrConflict` 409; `bridgeipc.NewError` only where the code itself is the point. → "Request Validation".
5. **Context and errors.** `ctx` first and propagated; wrap errors with operation context; no panics for runtime or input failures. Log or return, never both — the router logs every failed route. → "Context".
6. **Logging.** `log/slog`; lowercase constant message, no trailing punctuation, no `[Tag]` or `Prefix:`; context in attributes; error key is `error`. One Info line per completed host mutation, past tense, identifiers as attributes; reads and progress are Debug. → "Logging". `backend/common/logging/style_test.go` enforces this.
7. **Host access.** Go APIs and existing D-Bus/service abstractions over shell. A subprocess only through the existing context-aware command path, arguments separate, cancellation honoured, stderr returned with the error. Optional host facilities are capabilities, not hard failures.
8. **Contract.** Run `make generate`. Never hand-edit `frontend/src/api/generated/*` or `frontend/src/routeTree.gen.ts`. The frontend consumer uses the generated query options and route helpers; `docs/api-contract.md` → "Frontend Shape" and "Choosing a member".
9. **Tests.** Add a case in `handlers_test.go` beside the binding (examples: `handlers/tasks`, `handlers/wireguard`, `handlers/virt`). Mark tests that need systemd, Docker, libvirt or root so they stay out of the unit run.
10. **Docs.** Update `docs/api-contract.md` if a contract shape changed, and the domain doc under `docs/` if behaviour changed.
11. **Gate.** `make test-quiet` (backend plus generated contract changed). Then the handoff commit message, or `/finish-check`.
