# TODO

Each active plan has its own document:

- [API reliability, recovery, and notifications](./api-reliability-roadmap.md)
- [Frontend task state unification](./frontend-task-state-unification.md)
- [Frontend test coverage](./e2e-testing.md)
- [Notifications](./notifications.md)
- [VM bridged networking](./vm-bridge-networking-plan.md)

## Dependency maintenance

- [x] Added the scoped `@vitejs/plugin-react` → `oxc-transform-react` npm
  override. It references the root devDependency spec (`$oxc-transform-react`),
  so compiler bumps need no override edit.
- [ ] Remove that override once the plugin's published peer-dependency range
  supports the selected compiler version ([upstream issue](https://github.com/vitejs/vite-plugin-react/issues/1437)).
  Refresh the lockfile and verify a fresh install with
  `make setup '.SHELLFLAGS=-ec'`, then run `make ci-frontend-deps` and
  `make check-frontend-quiet` before closing this item.

Finished and rejected work is retained in [Completed or closed TODOs](./completed.md).
