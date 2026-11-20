// Rules for the addresses Docker waits for at startup, the same ones the
// backend applies (ParseDockerHostIPs). The backend stays authoritative: this
// only saves a round trip for the common mistakes.

const IPV4_OCTET = "(?:25[0-5]|2[0-4]\\d|1\\d\\d|[1-9]?\\d)";
const IPV4 = new RegExp(`^(?:${IPV4_OCTET}\\.){3}${IPV4_OCTET}$`);
const IPV6_CHARACTERS = /^[0-9a-f:.]+$/i;
const IPV4_MAPPED = /^::ffff:([0-9a-f]{1,4}):([0-9a-f]{1,4})$/;

/**
 * The form the config stores an address in, or null when the text is not an
 * IP address: IPv4 as a strict dotted quad, IPv6 lower-case and compressed,
 * and an IPv4-mapped IPv6 address as its IPv4 form.
 */
export function canonicalHostIP(value: string): string | null {
  const input = value.trim();
  if (IPV4.test(input)) return input;
  if (!input.includes(":") || !IPV6_CHARACTERS.test(input)) return null;

  // The URL parser reads an IPv6 host and writes it back canonically.
  let host: string;
  try {
    host = new URL(`http://[${input}]/`).hostname;
  } catch {
    return null;
  }
  const address = host.slice(1, -1);
  const mapped = IPV4_MAPPED.exec(address);
  if (!mapped) return address;
  const high = parseInt(mapped[1], 16);
  const low = parseInt(mapped[2], 16);
  return `${high >> 8}.${high & 255}.${low >> 8}.${low & 255}`;
}

function hostIPError(input: string, canonical: string | null): string {
  if (input === "") return "IP address is required.";
  if (input.includes("%")) {
    return "An address with a zone (%) cannot be used.";
  }
  if (canonical === null) return "Enter an IPv4 or IPv6 address.";
  if (canonical === "0.0.0.0" || canonical === "::") {
    return "Enter a specific address, not the one that means every address.";
  }
  if (canonical.startsWith("127.") || canonical === "::1") {
    return "A loopback address is always assigned.";
  }
  return "";
}

/**
 * Checks the rows of the host IP list. `hostIPs` holds the canonical value
 * of each row ("" for a row that is not an address) and `errors` the message
 * for each row ("" for a row that is fine).
 */
export function validateHostIPs(drafts: readonly string[]): {
  hostIPs: string[];
  errors: string[];
} {
  const hostIPs: string[] = [];
  const errors: string[] = [];
  const seen = new Set<string>();

  for (const draft of drafts) {
    const input = draft.trim();
    const canonical = canonicalHostIP(input);
    let error = hostIPError(input, canonical);
    if (!error && canonical !== null) {
      if (seen.has(canonical)) error = "IP address is already listed.";
      seen.add(canonical);
    }
    hostIPs.push(canonical ?? "");
    errors.push(error);
  }

  return { hostIPs, errors };
}
