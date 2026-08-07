/** "coretemp3" → "Coretemp 3", "package" → "Package". */
export const formatSensorLabel = (key: string): string => {
  const match = key.match(/^([a-zA-Z]+)(\d+)$/);
  if (match)
    return `${match[1].charAt(0).toUpperCase() + match[1].slice(1)} ${match[2]}`;
  return key.charAt(0).toUpperCase() + key.slice(1);
};
