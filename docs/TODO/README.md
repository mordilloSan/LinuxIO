# TODO

Open work is in one list: the [backlog](./backlog.md). It covers the backend,
the API, jobs, scheduled scripts and consistency.

Plans for things that are not built yet each have their own document:

- [Docker: wait for published host IPs](./docker-wait-host-ips.md)
- [Frontend test coverage](./e2e-testing.md)
- [Package-managed installation](./packaging.md)
- [Watched conditions: checks that raise alerts on a schedule](./watched-conditions.md)

## Dependency maintenance

- [x] Added the scoped `@vitejs/plugin-react` → `oxc-transform-react` npm
  override. It references the root devDependency spec (`$oxc-transform-react`),
  so compiler bumps need no override edit.
- [ ] Remove that override once the plugin's published peer-dependency range
  supports the selected compiler version ([upstream issue](https://github.com/vitejs/vite-plugin-react/issues/1437)).
  Refresh the lockfile and verify a fresh install with
  `make setup-frontend '.SHELLFLAGS=-ec'`, then run `make ci-frontend-deps` and
  `make check-frontend-quiet` before closing this item.
