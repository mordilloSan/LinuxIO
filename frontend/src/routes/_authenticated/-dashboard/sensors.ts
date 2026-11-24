/**
 * "coretemp3" → "Coretemp 3", "package" → "Package", and on a host with
 * several CPU sockets "package1core3" → "Package 1 Core 3".
 */
export const formatSensorLabel = (key: string): string =>
  key
    .replace(/([a-zA-Z])(?=\d)|(\d)(?=[a-zA-Z])/g, "$1$2 ")
    .replace(/(^|\s)[a-z]/g, (start) => start.toUpperCase());
