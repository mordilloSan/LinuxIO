# Notification System

> **Status: Planned** - This document describes a server-side notification system that is not yet implemented. The current navbar "notifications" dropdown shows recent toast history only (client-side).

## Overview
LinuxIO notifications are server-side, per-user events delivered in real time over the existing stream mux. The system supports read/unread state, retention, severity levels, and source tags. Clients render the latest items in the navbar dropdown and a full list page; toasts are emitted based on user preferences.

## Goals
- Persist notifications per user (no loss on reload).
- Push updates in real time via the yamux/WebSocket relay.
- Track read/unread state for badges and filtering.
- Support severity (info/warn/error) and source tags (docker/disk/system).
- Allow user-configurable toast rules.

## Architecture
```
Sources (docker/disk/system)
        ↓
Notification Service (create/dedupe/retain)
        ↓
Storage (per-user)
        ↓
Push Stream (notifications)
        ↓
Frontend Store
   ↙           ↘
Dropdown      Notifications Page
        ↘
       Toasts (user policy)
```

## Data Model

### Notification
- id (uuid/string)
- user_id
- created_at
- read_at (nullable)
- severity: info | warn | error
- source: docker | disk | system | custom
- title
- message
- meta (json): { href, label, extra }
- dedupe_key (optional)
- expires_at (optional)

### NotificationPreference
- user_id
- toast_policy: all | critical | none (or per-severity map)
- retention_days / retention_count (if user-scoped)

## Streams and API

### Push Stream
Stream type: `notifications`

Events:
- `notification.created`
- `notification.updated` (e.g., read state)
- `notification.deleted`
- `notification.counts` (optional aggregate updates)

Example payload:
```json
{
  "type": "notification.created",
  "notification": {
    "id": "abc123",
    "title": "Low disk space",
    "message": "Root has 4% free.",
    "severity": "warn",
    "source": "disk",
    "created_at": "2025-01-01T12:00:00Z",
    "meta": { "href": "/storage", "label": "Open storage" }
  }
}
```

### API Endpoints (server-side)
- `GET /notifications?status=unread&limit=50&cursor=...`
- `POST /notifications/read` (ids)
- `POST /notifications/unread` (ids)
- `POST /notifications/read-all`
- `POST /notifications/clear`
- `GET /notifications/preferences`
- `POST /notifications/preferences`

## Retention
- Default policy: keep latest N (e.g., 200) or up to X days.
- Cleanup runs periodically and on insert if over capacity.

## Client Behavior
- Subscribe to `notifications` stream on login.
- Maintain local store (sorted by created_at).
- Badge count = unread items.
- Dropdown shows latest 5.
- Full page offers filters (all/unread, severity, source).
- Toasts fire only if allowed by user policy and severity.

## Sources (initial)
- Disk: low space thresholds and failures.
- Docker: update/health status changes (initial stub).
- System: critical service failures (planned).

## Security and Privacy
- All notifications scoped to authenticated user.
- Sanitize meta fields used for links.
- Enforce access control on list and action endpoints.

## Extending with New Sources
1. Add source tag.
2. Emit notification via Notification Service with dedupe_key.
3. Include `meta.href` for deep links where applicable.
