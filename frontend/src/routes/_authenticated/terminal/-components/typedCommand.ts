// A command the Assistant asks the terminal to type for the user to run. It is
// text an agent wrote, on its way into a shell, so it is reduced to printable
// characters and newlines: nothing in it can press Enter, send an interrupt or
// end a bracketed paste early.

const MAX_LENGTH = 8000;
const PASTE_START = "\u001b[200~";
const PASTE_END = "\u001b[201~";

const isPrintable = (char: string) => {
  const code = char.codePointAt(0) ?? 0;
  return code >= 0x20 && code !== 0x7f && !(code >= 0x80 && code <= 0x9f);
};

/** The command in router history state, or null when it is not usable. */
export function typedCommandFromState(value: unknown): string | null {
  if (typeof value !== "string" || value.length > MAX_LENGTH) return null;
  const command = Array.from(
    value.replace(/\r\n/g, "\n").replace(/\t/g, "    "),
  )
    .filter((char) => char === "\n" || isPrintable(char))
    .join("")
    .replace(/^\n+|\n+$/g, "");
  return command.trim() === "" ? null : command;
}

/**
 * What to write to the PTY so the command is typed and not run. One line is
 * typed as it is. Several lines go as one bracketed paste, which a shell
 * inserts whole; where the shell has not turned bracketed paste on it would
 * run every line but the last, so the answer is null and the caller waits.
 */
export function typedCommandInput(
  command: string,
  bracketedPaste: boolean,
): string | null {
  if (!command.includes("\n")) return command;
  if (!bracketedPaste) return null;
  return `${PASTE_START}${command.replace(/\n/g, "\r")}${PASTE_END}`;
}
