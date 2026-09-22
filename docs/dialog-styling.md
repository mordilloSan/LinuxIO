# Dialog styling

`AppDialog` owns modal surfaces, typography, title/content/action spacing and
responsive margins in `components/ui/app-dialog.css`. `GeneralDialog` keeps the
existing import path and delegates to that parent. `AppFullscreenDialog` shares
the same typography and internal structure.

Use `AppDialogTitle`, `AppDialogContent` and `AppDialogActions` inside the parent.
The title supplies the accessible name through the parent context. Provide an
explicit `aria-label` for a dialog with a custom toolbar or no visible title.

## Forms and actions

- Field values and empty field labels use 14 px (`0.875rem`) inside dialogs.
  `small` controls keep compact padding and use the same field font.
- Floating labels use 12 px; titles use 16 px and body copy uses 13 px.
- Use `app-dialog-fields` for a vertical form. For a grid, keep the feature's
  column layout and use `--app-dialog-field-gap` (16 px).
- Use `--app-dialog-section-gap` (24 px) between form sections.
- A form around the title, content and actions uses `app-dialog-form` so the
  content can scroll while the actions remain visible.
- The primary action uses `variant="contained"`. Destructive actions also use
  `color="error"`. Secondary actions use the regular text or outlined variant.
- The shared action row wraps on narrow screens. Do not recreate a footer with
  local font, padding or hover effects.

## Specialized content

Use `AppDialogContent flush` for editors, terminals and log viewports that need
zero padding. Feature code can own grid columns, viewport dimensions and the
content's monospace font or background. Keep the surrounding title, surface and
actions in the shared components.

Do not set dialog field font variables in a feature or override title/footer
paint and padding. Change the parent when a common style needs adjustment.
Controls outside a dialog keep their existing sizes.

Native browser confirmations are also shared dialogs: deleting a scheduled task
and starting a LinuxIO update both require an explicit confirmation.

## Verification

`make check-frontend-quiet` includes accessible title tests and a source guard
against local chrome overrides and native alert/confirm/prompt calls.
`make test-frontend-browser-quiet` includes the
dialog gallery in both color schemes at 320 px and desktop width, mixed control
sizes, select portals, fullscreen inheritance, focus restoration and narrow
confirmation actions.

To update only the dialog screenshots after a deliberate style change:

```sh
make test-frontend-browser-quiet PLAYWRIGHT_ARGS='src/test/browser/dialog-styling.spec.ts --update-snapshots'
```

Review the images before accepting the new baselines. Browser fixtures do not
validate host mutations such as creating a VM, changing networking or deleting
files; verify those flows on a suitable host.

## Manual review

For each family below, compare light/dark themes and desktop/mobile widths. Field
values should have the same size across inputs, selects and autocomplete. Check
floating labels, helpers, validation messages, long names, focus indication and
button text during loading. On a short screen, scroll the content and confirm
that the actions remain reachable. Test Cancel, Escape and focus returning to the
trigger; pending operations must retain their existing close restrictions.

| Family | Dialogs and flows to check |
| --- | --- |
| WireGuard | Create interface (name, port, CIDR, DNS, peers, NIC); peer QR code. |
| Accounts | Create/edit user and group; password; group members; terminate session. |
| Network | Create bridge; move host IP to bridge; confirmation/recovery during handoff. |
| Docker resources | Create/edit/recreate container; create network/volume; connect/disconnect container; remove, prune and volume archive confirmations. |
| Docker stacks | Stack setup, Compose editor, unsaved changes, post-save action, delete stack, operation progress and container update. |
| VM | Create VM, template/image selection, delete confirmations and console (title, status, close button, viewport). |
| Shares and storage | Add/edit SMB and NFS mounts; folder shares; create/resize logical volume and delete confirmations. |
| Files | Create/rename, compression format, delete, owner/group/permissions, details, upload conflicts, editor save conflicts and unsaved changes. |
| System | Hostname, date/time, scheduled task creation/editing/deletion, failed logins, LinuxIO update confirmation and progress. |
| Tools and output | Capability installation, indexer status, logs, Docker terminal and developer tools; check viewport sizing and nested dialogs. |

The browser suite covers shared presentation and selected flows with simulated
data. Real backend operations in this checklist still require host validation.
