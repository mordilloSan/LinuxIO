/** Build the same-origin URL for a prepared archive download. */
export function nativeArchiveDownloadUrl(taskId: string): string {
  return `/api/download?taskId=${encodeURIComponent(taskId)}`;
}

/** Build the same-origin URL for a direct single-file download. */
export function nativeFileDownloadUrl(path: string): string {
  return `/api/download?path=${encodeURIComponent(path)}`;
}

/** Start a browser-managed download without reading the response in JavaScript. */
export function triggerNativeArchiveDownload(taskId: string): void {
  triggerDownload(nativeArchiveDownloadUrl(taskId));
}

/** Start a browser-managed download for a single file path. */
export function triggerNativeFileDownload(path: string): void {
  triggerDownload(nativeFileDownloadUrl(path));
}

function triggerDownload(url: string): void {
  const link = document.createElement("a");
  link.href = url;
  link.download = "";
  link.style.display = "none";
  document.body.appendChild(link);
  link.click();
  link.remove();
}
