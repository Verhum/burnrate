import type { Attachment } from "@/lib/api/types";
import { client } from "@/lib/api/client";

interface AttachmentItemProps {
  attachment: Attachment;
  onDelete: (id: number) => void;
}

export function AttachmentItem({ attachment, onDelete }: AttachmentItemProps) {
  const isImage = attachment.content_type?.startsWith("image/");
  const dataUrl = client.attachmentDataUrl(attachment.id);

  return (
    <div className="relative group bg-elevated overflow-hidden">
      <div className="aspect-square flex items-center justify-center">
        {isImage ? (
          <img src={dataUrl} alt={attachment.filename} className="w-full h-full object-cover" crossOrigin="anonymous" />
        ) : (
          <span className="text-xs text-muted px-2 text-center truncate">{attachment.filename}</span>
        )}
      </div>
      <button
        onClick={() => onDelete(attachment.id)}
        className="absolute top-1 right-1 w-5 h-5 bg-raised text-amber text-xs
          flex items-center justify-center opacity-0 group-hover:opacity-100
          transition-opacity cursor-pointer border-none font-mono hover:bg-warm"
      >
        ×
      </button>
    </div>
  );
}
