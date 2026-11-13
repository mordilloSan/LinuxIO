/**
 * Asks the footer bell to open its tray from anywhere, such as the sign-in
 * notice's Show action. The bell reads the latest request with
 * useSyncExternalStore and opens its tray when a new one arrives.
 */
export interface NotificationsPanelRequest {
  /** When the tray was asked to open, in milliseconds since the epoch. */
  at: number;
}

let request: NotificationsPanelRequest | null = null;
const listeners = new Set<() => void>();

export function openNotificationsPanel(): void {
  request = { at: Date.now() };
  for (const listener of listeners) listener();
}

export function subscribeNotificationsPanel(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** The latest request, or null when the tray was never asked to open. */
export function notificationsPanelRequest(): NotificationsPanelRequest | null {
  return request;
}
