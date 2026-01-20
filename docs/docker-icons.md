# Docker Container Icons

LinuxIO automatically displays icons for Docker containers and compose stacks.

## Quick Reference

| Format | Example | Source |
|--------|---------|--------|
| Plain name | `nginx` | Dashboard Icons → Simple Icons → Docker |
| Dashboard Icons | `di:plex` | [homarr-labs/dashboard-icons](https://github.com/homarr-labs/dashboard-icons) |
| Simple Icons | `si:react` | [simpleicons.org](https://simpleicons.org) |
| URL | `https://example.com/icon.svg` | Any HTTP(S) URL |
| Local file | `my-icon.svg` | `/run/linuxio/icons/user/` |

## Default Behavior

When no icon is specified, LinuxIO automatically derives one using this fallback chain:

```
Service/Container Name → Dashboard Icons → Simple Icons → Docker whale
```

2. If no image, use **service name** (compose) or **container name** (standalone)
3. Search [Dashboard Icons](https://github.com/homarr-labs/dashboard-icons/tree/main/svg) (3,500+ self-hosted app icons)
4. Search [Simple Icons](https://simpleicons.org) (3,000+ brand icons)
5. Fallback to Docker whale icon

**Most containers will automatically get the correct icon without any configuration.**

## Defining Icons

### Per-Container

Use the `io.linuxio.container.icon` label:

```yaml
services:
  webapp:
    image: nginx:alpine
    labels:
      - "io.linuxio.container.icon=nginx"
```

### Per-Stack

Use the `x-linuxio-stack` extension:

```yaml
x-linuxio-stack:
  icon: "nextcloud"

services:
  app:
    image: nextcloud:latest
```

## Icon Formats

### Plain Name (Recommended)

Just use the application name:

```yaml
labels:
  - "io.linuxio.container.icon=nginx"
  - "io.linuxio.container.icon=plex"
  - "io.linuxio.container.icon=nextcloud"
```

### Explicit Source

Force a specific icon source:

```yaml
labels:
  # Dashboard Icons (self-hosted apps)
  - "io.linuxio.container.icon=di:jellyfin"

  # Simple Icons (brands/technologies)
  - "io.linuxio.container.icon=si:react"
```

### Custom URL

```yaml
labels:
  - "io.linuxio.container.icon=https://example.com/my-icon.svg"
```

### Local File

Place icon in `/run/linuxio/icons/user/` and reference by filename:

```yaml
labels:
  - "io.linuxio.container.icon=my-custom-icon.svg"
```

## Complete Example

```yaml
x-linuxio-stack:
  icon: "plex"

services:
  # Auto-derived: linuxserver/plex → plex icon
  plex:
    image: linuxserver/plex:latest
    labels:
      - "io.linuxio.container.url=http://localhost:32400"

  # Auto-derived: postgres → postgres icon
  database:
    image: postgres:15-alpine

  # Explicit Simple Icon for React brand
  frontend:
    image: node:20-alpine
    labels:
      - "io.linuxio.container.icon=si:react"

  # Unknown image → Docker whale fallback
  utility:
    image: busybox:latest
    command: sleep infinity
```

## Icon Caching

Icons are cached for 24 hours in `/run/linuxio/icons/`:

- `dashboard-icons/` - Dashboard Icons cache
- `simple-icons/` - Simple Icons cache
- `url-cache/` - URL icons (hashed filenames)
- `user/` - User-provided local icons

## Additional Labels

| Label | Description |
|-------|-------------|
| `io.linuxio.container.icon` | Container icon |
| `io.linuxio.container.url` | Clickable URL for the container |

## Browse Available Icons

- **Dashboard Icons**: https://github.com/homarr-labs/dashboard-icons/tree/main/svg
- **Simple Icons**: https://simpleicons.org

## Troubleshooting

| Problem | Solution |
|---------|----------|
| Icon not showing | Check if icon exists in Dashboard Icons or Simple Icons |
| Wrong icon | Add explicit `io.linuxio.container.icon` label |
| Slow first load | Normal - icons are fetched from CDN on first request, then cached |
