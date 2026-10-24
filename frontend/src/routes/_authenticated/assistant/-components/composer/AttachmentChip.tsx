import { Icon } from "@iconify/react";
import { useState } from "react";

import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppButton from "@/components/ui/AppButton";
import { AppDialogContent, AppDialogTitle } from "@/components/ui/AppDialog";

import { type Attachment, attachmentName, lineCount } from "../attachments";

/** The text an attachment can show in full, if it has any. */
function previewText(attachment: Attachment) {
  if (attachment.kind === "text") return attachment.text;
  if (attachment.kind === "file" && attachment.block.type === "resource") {
    return "text" in attachment.block.resource
      ? attachment.block.resource.text
      : null;
  }
  return null;
}

/**
 * One attachment as a chip: a thumbnail or icon, the name, and a detail such
 * as the image size or the line count. Clicking it opens the content in a
 * dialog; a linked file has nothing to show, so its chip is not a button.
 * In the composer it has a remove cross; in the transcript it does not.
 */
export default function AttachmentChip({
  attachment,
  onRemove,
}: {
  attachment: Attachment;
  onRemove?: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [size, setSize] = useState<string | null>(null);
  const name = attachmentName(attachment);
  const text = previewText(attachment);
  const meta =
    attachment.kind === "image"
      ? size
      : text !== null
        ? `${lineCount(text)} ${lineCount(text) === 1 ? "line" : "lines"}`
        : null;
  const canOpen = attachment.kind === "image" || text !== null;
  const body = (
    <>
      {attachment.kind === "image" ? (
        <img
          alt={name}
          className="assistant__attachment-thumb"
          onLoad={(event) =>
            setSize(
              `${event.currentTarget.naturalWidth}×${event.currentTarget.naturalHeight}`,
            )
          }
          src={attachment.previewUrl}
        />
      ) : (
        <Icon
          icon={
            attachment.kind === "text"
              ? "mdi:text-box-outline"
              : "mdi:file-outline"
          }
          width={16}
        />
      )}
      <span className="assistant__attachment-name">{name}</span>
      {meta ? <span className="assistant__attachment-meta">{meta}</span> : null}
    </>
  );
  return (
    <span className="assistant__attachment">
      {canOpen ? (
        <AppButton
          aria-label={`Open ${name}`}
          className="assistant__attachment-open"
          color="inherit"
          onClick={() => setOpen(true)}
        >
          {body}
        </AppButton>
      ) : (
        <span
          className="assistant__attachment-open"
          title={attachment.kind === "file" ? attachment.path : undefined}
        >
          {body}
        </span>
      )}
      {onRemove ? (
        <AppActionIconButton
          ariaLabel={`Remove ${name}`}
          icon="mdi:close"
          iconSize={14}
          onClick={onRemove}
          tooltip={false}
        />
      ) : null}
      <GeneralDialog
        fullWidth
        maxWidth="md"
        onClose={() => setOpen(false)}
        open={open}
      >
        <AppDialogTitle>{name}</AppDialogTitle>
        <AppDialogContent>
          {attachment.kind === "image" ? (
            <img
              alt={name}
              className="assistant__preview-image"
              src={attachment.previewUrl}
            />
          ) : (
            <pre className="assistant__preview-text">{text}</pre>
          )}
        </AppDialogContent>
      </GeneralDialog>
    </span>
  );
}
