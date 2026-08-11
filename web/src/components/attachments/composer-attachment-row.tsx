"use client";

import type { ComposerAttachment } from "@/hooks/use-composer-attachments";
import { ComposerAttachmentChip } from "./composer-attachment-chip";

interface ComposerAttachmentRowProps {
  attachments: readonly ComposerAttachment[];
  onRemove: (key: string) => void;
  disabled?: boolean;
}

/**
 * The chip row a composer shows above its textarea. Renders nothing when empty,
 * so a caller can drop it in unconditionally.
 */
export function ComposerAttachmentRow({
  attachments,
  onRemove,
  disabled,
}: ComposerAttachmentRowProps) {
  if (attachments.length === 0) return null;
  return (
    <div className="flex flex-wrap gap-1.5">
      {attachments.map((a) => (
        <ComposerAttachmentChip
          key={a.key}
          attachment={a}
          onRemove={onRemove}
          disabled={disabled}
        />
      ))}
    </div>
  );
}
