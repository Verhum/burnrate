"use client";

import type { ComposerAttachment } from "@/hooks/use-composer-attachments";
import { Spinner } from "@/components/ui";

interface ComposerAttachmentChipProps {
  attachment: ComposerAttachment;
  onRemove: (key: string) => void;
  disabled?: boolean;
}

/**
 * One pasted/dropped image above a composer: spinner while it uploads,
 * thumbnail once it lands, an inline reason when it fails. Removable either way
 * — an error chip is information, not a blocker.
 */
export function ComposerAttachmentChip({
  attachment,
  onRemove,
  disabled,
}: ComposerAttachmentChipProps) {
  const failed = attachment.status === "error";

  return (
    <div
      className={`relative group flex items-center gap-1.5 pl-1 pr-5 py-1 font-mono text-[9px]
        ${failed ? "bg-danger/15 text-danger" : "bg-raised text-dim"}`}
      title={failed ? `${attachment.filename}: ${attachment.error}` : attachment.filename}
    >
      <span className="w-8 h-8 flex items-center justify-center bg-surface shrink-0 overflow-hidden">
        {attachment.status === "uploading" ? (
          <Spinner size="sm" />
        ) : attachment.previewUrl ? (
          // eslint-disable-next-line @next/next/no-img-element -- a blob: object URL, not an optimizable asset
          <img
            src={attachment.previewUrl}
            alt={attachment.filename}
            className="w-full h-full object-cover"
          />
        ) : (
          <span className="text-[8px]">!</span>
        )}
      </span>
      <span className="max-w-[8rem] truncate">
        {failed ? attachment.error : attachment.filename}
      </span>
      <button
        type="button"
        onClick={() => onRemove(attachment.key)}
        disabled={disabled}
        aria-label={`Remove ${attachment.filename}`}
        className="absolute top-0.5 right-0.5 w-3.5 h-3.5 flex items-center justify-center
          bg-surface text-amber border-none cursor-pointer font-mono text-[9px]
          hover:bg-warm hover:text-primary transition-colors disabled:opacity-40
          disabled:cursor-not-allowed"
      >
        ×
      </button>
    </div>
  );
}
