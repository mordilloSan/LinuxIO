export const parseDockerKeyValueLines = (text: string) => {
  const values: Record<string, string> = {};
  for (const [index, rawLine] of text.split("\n").entries()) {
    const line = rawLine.trim();
    if (!line) continue;
    const separator = line.indexOf("=");
    const key = line.slice(0, separator).trim();
    if (separator < 1 || !key) {
      return {
        error: `Line ${index + 1} must use key=value.`,
        values: {},
      };
    }
    values[key] = line.slice(separator + 1).trim();
  }
  return { error: undefined, values };
};
