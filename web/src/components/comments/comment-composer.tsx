"use client";

import { useState } from "react";
import type { Comment } from "@/lib/api/types";
import { Button } from "@/components/ui";
import { ComposerAttachmentRow } from "@/components/attachments/composer-attachment-row";
import { client } from "@/lib/api/client";
import { composeBodyWithAttachments } from "@/lib/composer-attachments";
import { useComposerAttachments } from "@/hooks/use-composer-attachments";
import { VoiceDictation } from "./voice-dictation";

interface CommentComposerProps {
  taskId: number;
  isRunning?: boolean;
  /** The created comment, so the thread can prepend it without waiting for a refetch. */
  onCreated: (comment: Comment) => void;
}

/**
 * The add-a-comment box. It sits at the TOP of the thread (comments render
 * newest-first), so the chip row for pasted images goes above the textarea and
 * pushes nothing important off-screen.
 *
 * Screenshots paste or drop straight in: the image uploads to the task's
 * attachments immediately — independent of whether the comment itself is
 * accepted — and the submitted body gains a markdown image line per upload, so
 * it renders inline here and reaches the agent's next run via the runner's
 * `## Image Attachments` section.
 */
export function CommentComposer({ taskId, isRunning, onCreated }: CommentComposerProps) {
  const [body, setBody] = useState("");
  const [loading, setLoading] = useState(false);
  const {
    attachments,
    uploaded,
    uploading,
    dragging,
    remove,
    clear,
    onPaste,
    dropZoneProps,
  } = useComposerAttachments(taskId);

  // A screenshot with no prose is a complete comment.
  const canSubmit = body.trim() !== "" || uploaded.length > 0;

  const handleSubmit = async () => {
    if (!canSubmit || loading) return;
    setLoading(true);
    try {
      const created = await client.createComment(taskId, {
        body: composeBodyWithAttachments(body, uploaded),
      });
      setBody("");
      clear();
      onCreated(created);
    } catch {
      // ignore — the route 409s while a run is in flight, and the text stays
      // in the box so it can be sent again
    }
    setLoading(false);
  };

  return (
    <div className="flex flex-col gap-2" {...dropZoneProps}>
      <ComposerAttachmentRow attachments={attachments} onRemove={remove} disabled={loading} />
      <textarea
        value={body}
        onChange={(e) => setBody(e.target.value)}
        onPaste={onPaste}
        placeholder={
          isRunning
            ? "Add instruction for running agent... (paste a screenshot to attach it)"
            : "Add a comment... (paste a screenshot to attach it)"
        }
        className={`bg-elevated text-primary px-3 py-2 text-xs font-mono border-none outline-none
          resize-y min-h-[60px] placeholder:text-muted focus:bg-raised transition-colors
          ${dragging ? "shadow-[inset_0_0_0_1px_var(--color-amber)]" : ""}`}
      />
      <div className="flex justify-end items-center gap-2">
        {uploading && <span className="text-[9px] text-muted font-mono">uploading…</span>}
        <VoiceDictation
          onTranscript={(text) => setBody((prev) => (prev ? prev + " " + text : text))}
        />
        <Button size="sm" onClick={handleSubmit} disabled={loading || !canSubmit}>
          {isRunning ? "Send to Agent" : "Comment"}
        </Button>
      </div>
    </div>
  );
}
