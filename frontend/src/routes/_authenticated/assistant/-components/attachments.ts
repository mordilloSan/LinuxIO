import type * as acp from "@agentclientprotocol/sdk";

import { textBlock } from "@/api/acp";

export type Attachment =
  | {
      id: string;
      kind: "image";
      name: string;
      mimeType: string;
      /** base64, without the data: prefix */
      data: string;
      previewUrl: string;
    }
  | { id: string; kind: "file"; path: string; block: acp.ContentBlock };

const MAX_EMBED_CHARS = 200_000;
const MAX_IMAGE_BYTES = 5 * 1024 * 1024;

let counter = 0;
// randomUUID needs a secure context; the counter covers plain-http installs.
const newId = () =>
  typeof crypto.randomUUID === "function"
    ? crypto.randomUUID()
    : `attachment-${++counter}`;

export const baseName = (path: string) => path.split("/").pop() || path;

export const attachmentName = (attachment: Attachment) =>
  attachment.kind === "image" ? attachment.name : baseName(attachment.path);

/** Embeds a text file under 200 KB; anything larger or unreadable is sent as a link. */
export async function fileAttachment(
  path: string,
  readText: (path: string) => Promise<string>,
): Promise<Attachment> {
  const uri = `file://${path}`;
  try {
    const text = await readText(path);
    if (text.length <= MAX_EMBED_CHARS) {
      return {
        id: newId(),
        kind: "file",
        path,
        block: { type: "resource", resource: { uri, text } },
      };
    }
  } catch {
    // fall through to a link
  }
  return {
    id: newId(),
    kind: "file",
    path,
    block: { type: "resource_link", uri, name: baseName(path) },
  };
}

export function imageAttachment(file: File): Promise<Attachment> {
  if (file.size > MAX_IMAGE_BYTES) {
    return Promise.reject(
      new Error(`${file.name || "Image"} is over the 5 MB image limit.`),
    );
  }
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(reader.error);
    reader.onload = () => {
      const url = reader.result as string;
      resolve({
        id: newId(),
        kind: "image",
        name: file.name || "image",
        mimeType: file.type,
        data: url.slice(url.indexOf(",") + 1),
        previewUrl: url,
      });
    };
    reader.readAsDataURL(file);
  });
}

/** Images first, then files, then the text. */
export function buildPrompt(
  text: string,
  attachments: Attachment[],
): acp.ContentBlock[] {
  const images = attachments.flatMap((a) =>
    a.kind === "image"
      ? [
          {
            type: "image" as const,
            data: a.data,
            mimeType: a.mimeType,
          },
        ]
      : [],
  );
  const files = attachments.flatMap((a) =>
    a.kind === "file" ? [a.block] : [],
  );
  return [...images, ...files, textBlock(text)];
}
