# Docker Icons

LinuxIO displays icons for Docker containers, compose services, and compose stacks. Icons are resolved on demand by the backend and returned to the frontend as data URIs.

## Quick Reference

| Identifier | Example | Resolution |
|------------|---------|------------|
| Plain name | `nginx` | Dashboard Icons, then Simple Icons, then Docker icon |
| Dashboard Icons | `di:plex` | [homarr-labs/dashboard-icons](https://github.com/homarr-labs/dashboard-icons) |
| Dashboard Icons long form | `dashboard-icon:plex` | Same as `di:` |
| Simple Icons | `si:react` | [simpleicons.org](https://simpleicons.org) |
| Simple Icons long form | `simple-icon:react` | Same as `si:` |
| URL | `https://example.com/icon.svg` | Downloads an HTTP(S) icon |
| Local file | `my-icon.svg` | Reads from `/run/linuxio/icons/user/` |

Supported local-file extensions are `.svg`, `.png`, `.jpg`, `.jpeg`, and `.webp`.

## Default Resolution

When a container or compose service does not set an icon label, LinuxIO derives an identifier from the Docker metadata:

1. Compose services use the compose service name.
2. Standalone containers use the container name without the leading `/`.
3. Compose containers listed in the container view use the service name when the container name follows Docker Compose's generated `project-service-index` pattern; otherwise they use the container name.

The derived identifier is resolved as:

```text
derived name -> Dashboard Icons -> Simple Icons -> Docker icon
```

LinuxIO does not derive icons from the Docker image name. If the service or container name is generic, set `io.linuxio.container.icon` explicitly.

## Defining Icons

Set a per-container or per-service icon with the `io.linuxio.container.icon` label:

```yaml
services:
  web:
    image: nginx:alpine
    labels:
      - "io.linuxio.container.icon=nginx"
```

Set a compose stack icon with the top-level `x-linuxio-stack.icon` extension:

```yaml
x-linuxio-stack:
  icon: "nextcloud"

services:
  app:
    image: nextcloud:latest
```

## Identifier Formats

### Plain Names

Plain names are the easiest option for most apps:

```yaml
labels:
  - "io.linuxio.container.icon=nginx"
  - "io.linuxio.container.icon=plex"
  - "io.linuxio.container.icon=nextcloud"
```

LinuxIO tries Dashboard Icons first, then Simple Icons, then the Docker icon.

### Explicit Sources

Use a prefix when a name exists in more than one source or when you want to skip the derived-source lookup:

```yaml
labels:
  - "io.linuxio.container.icon=di:jellyfin"
  - "io.linuxio.container.icon=dashboard-icon:jellyfin"
  - "io.linuxio.container.icon=si:react"
  - "io.linuxio.container.icon=simple-icon:react"
```

If an explicit Dashboard Icons, Simple Icons, or URL icon cannot be fetched, LinuxIO falls back to the Docker icon.

### URL Icons

```yaml
labels:
  - "io.linuxio.container.icon=https://example.com/my-icon.svg"
```

URL icons are fetched with a 10 second timeout and a 5 MB response-size limit.

### Local Files

Place the icon under `/run/linuxio/icons/user/` and reference it by filename:

```yaml
labels:
  - "io.linuxio.container.icon=my-custom-icon.svg"
```

Local file icons are not fetched from the network and do not fall back to the Docker icon if the file is missing.

## Complete Example

```yaml
x-linuxio-stack:
  icon: "plex"

services:
  # Auto-derived from service name: "plex"
  plex:
    image: linuxserver/plex:latest
    labels:
      - "io.linuxio.container.url=http://localhost:32400"

  # Explicit icon because the service name is generic.
  database:
    image: postgres:15-alpine
    labels:
      - "io.linuxio.container.icon=si:postgresql"

  # Explicit Simple Icons source.
  frontend:
    image: node:20-alpine
    labels:
      - "io.linuxio.container.icon=si:react"

  # Unknown or unfetchable icons fall back to the Docker icon.
  worker:
    image: busybox:latest
    command: sleep infinity
```

## Container URLs

The Docker views also read `io.linuxio.container.url` for clickable container links:

```yaml
labels:
  - "io.linuxio.container.url=https://example.local"
```

When `io.linuxio.container.proxy.port` is present and no explicit URL is set, LinuxIO derives a local proxy URL from the icon/container name.

## Cache Layout

Remote icons are cached for 24 hours in `/run/linuxio/icons/`:

| Directory | Contents |
|-----------|----------|
| `dashboard-icons/` | Dashboard Icons cache |
| `simple-icons/` | Simple Icons cache |
| `url-cache/` | URL icons, stored by URL hash |
| `user/` | User-provided local icons |

Clearing the icon cache removes the remote-icon caches and keeps the `user/` directory.

## Backend Methods

The Docker bridge exposes these icon methods:

| Method | Result |
|--------|--------|
| `get_icon_uri` | Returns `{ "uri": "data:..." }` for an identifier, using the cache when available |
| `get_icon` | Returns base64 icon bytes, using the cache when available |
| `get_icon_info` | Returns parsed type, identifier, and cache status |
| `clear_icon_cache` | Clears cached remote icons |

## Troubleshooting

| Problem | Fix |
|---------|-----|
| Wrong icon | Set `io.linuxio.container.icon` explicitly |
| Generic service name | Use a plain name or explicit source prefix, for example `si:postgresql` |
| Local icon missing | Confirm the file exists under `/run/linuxio/icons/user/` |
| Slow first load | Icons are fetched on first use and then cached for 24 hours |
