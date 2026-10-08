/** Rejects (never throws) when the Clipboard API is missing, e.g. plain-HTTP LAN origins. */
export const copyToClipboard = (text: string): Promise<void> => {
  if (!text) return Promise.resolve();
  if (!navigator.clipboard) {
    return Promise.reject(new Error("Clipboard is unavailable"));
  }
  return navigator.clipboard.writeText(text);
};
