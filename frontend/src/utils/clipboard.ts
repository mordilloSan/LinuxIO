export const copyToClipboard = (text: string): Promise<void> =>
  text ? navigator.clipboard.writeText(text) : Promise.resolve();
