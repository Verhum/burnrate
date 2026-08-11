"use client";

import { useState, useEffect, useCallback } from "react";
import type { Attachment } from "@/lib/api/types";
import { Button } from "@/components/ui";
import { client } from "@/lib/api/client";
import { AttachmentItem } from "./attachment-item";

interface AttachmentGalleryProps {
  taskId: number;
}

export function AttachmentGallery({ taskId }: AttachmentGalleryProps) {
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [uploading, setUploading] = useState(false);

  const fetchAttachments = useCallback(async () => {
    try {
      const data = await client.listAttachments(taskId);
      setAttachments(data);
    } catch {
      // ignore
    }
  }, [taskId]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async fetch: setState runs after an await, not synchronously
    fetchAttachments();
  }, [fetchAttachments]);

  const handleUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    setUploading(true);
    try {
      await client.uploadAttachment(taskId, file);
      await fetchAttachments();
    } catch {
      // ignore
    }
    setUploading(false);
    e.target.value = "";
  };

  const handleDelete = async (id: number) => {
    try {
      await client.deleteAttachment(id);
      await fetchAttachments();
    } catch {
      // ignore
    }
  };

  return (
    <div className="flex flex-col gap-2">
      {attachments.length === 0 ? (
        <p className="text-xs text-muted text-center py-4">No attachments</p>
      ) : (
        <div className="grid grid-cols-3 gap-2">
          {attachments.map((a) => (
            <AttachmentItem key={a.id} attachment={a} onDelete={handleDelete} />
          ))}
        </div>
      )}
      <div className="flex justify-end">
        <label className="cursor-pointer">
          <input
            type="file"
            onChange={handleUpload}
            className="hidden"
            disabled={uploading}
          />
          <span className="inline-block px-3 py-1.5 bg-raised text-dim text-[10px] font-bold uppercase
            tracking-wider font-mono cursor-pointer hover:bg-warm hover:text-primary transition-colors">
            {uploading ? "Uploading..." : "Upload"}
          </span>
        </label>
      </div>
    </div>
  );
}
