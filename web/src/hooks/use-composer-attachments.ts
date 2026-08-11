"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { client } from "@/lib/api/client";
import { apiErrorMessage } from "@/lib/api/errors";
import {
  dragHasFiles,
  imageFilesFrom,
  uploadRejection,
  type UploadedAttachmentRef,
} from "@/lib/composer-attachments";

export interface ComposerAttachment {
  /** Stable across the upload's lifetime; the server id only exists once it lands. */
  key: string;
  filename: string;
  /** Object URL for the local thumbnail, "" when the file never became one. */
  previewUrl: string;
  status: "uploading" | "done" | "error";
  /** Set once the upload succeeds — this is what the markdown line points at. */
  id?: number;
  /** Set on failure; the chip shows it and submitting stays allowed. */
  error?: string;
}

/**
 * Upload state and paste/drop wiring for images dropped into a text composer.
 * One implementation, two callers: the request reply form and the task comment
 * box (`components/comments/comment-composer.tsx`).
 *
 * Uploads start the moment the image arrives — before the message is submitted
 * — so the human sees a real thumbnail and the file is already on the task by
 * the time the agent's next run reads it. That is also why the comment route's
 * "no comments while running" 409 is irrelevant here: the attachment lands
 * regardless of whether the comment does.
 *
 * A failed upload stays in the list as an error chip rather than vanishing or
 * blocking the message: the text is still worth sending.
 */
export function useComposerAttachments(taskId: number) {
  const [attachments, setAttachments] = useState<ComposerAttachment[]>([]);
  const current = useRef<ComposerAttachment[]>([]);
  current.current = attachments;
  const [dragging, setDragging] = useState(false);
  const nextKey = useRef(0);
  // Object URLs outlive the render that made them, so revocation is tracked
  // separately — an unmount mid-upload must not leak them.
  const previews = useRef(new Set<string>());

  const revoke = useCallback((url: string) => {
    if (!url) return;
    previews.current.delete(url);
    URL.revokeObjectURL(url);
  }, []);

  useEffect(() => {
    const urls = previews.current;
    return () => {
      urls.forEach((u) => URL.revokeObjectURL(u));
      urls.clear();
    };
  }, []);

  const addFiles = useCallback(
    (files: readonly File[]) => {
      if (files.length === 0) return;

      const started = files.map((file) => {
        const key = `att-${nextKey.current++}`;
        const rejection = uploadRejection(file);
        let previewUrl = "";
        if (!rejection) {
          previewUrl = URL.createObjectURL(file);
          previews.current.add(previewUrl);
        }
        const entry: ComposerAttachment = rejection
          ? { key, filename: file.name, previewUrl, status: "error", error: rejection }
          : { key, filename: file.name, previewUrl, status: "uploading" };
        return { file, entry, rejection };
      });

      setAttachments((prev) => [...prev, ...started.map((s) => s.entry)]);

      for (const { file, entry, rejection } of started) {
        if (rejection) continue;
        void (async () => {
          try {
            const uploaded = await client.uploadAttachment(taskId, file);
            setAttachments((prev) =>
              prev.map((a) =>
                a.key === entry.key
                  ? { ...a, status: "done", id: uploaded.id, filename: uploaded.filename }
                  : a
              )
            );
          } catch (err) {
            setAttachments((prev) =>
              prev.map((a) =>
                a.key === entry.key
                  ? { ...a, status: "error", error: apiErrorMessage(err) }
                  : a
              )
            );
          }
        })();
      }
    },
    [taskId]
  );

  const remove = useCallback(
    (key: string) => {
      // Read from the mirror, not from inside the updater: the updater can be
      // called twice (StrictMode) and deleting server-side twice is not free.
      const removed = current.current.find((a) => a.key === key);
      setAttachments((prev) => prev.filter((a) => a.key !== key));
      if (!removed) return;
      revoke(removed.previewUrl);
      if (removed.status === "done" && removed.id !== undefined) {
        // Best effort: the chip is already gone, and an orphaned attachment is
        // visible in the task's gallery rather than lost.
        void client.deleteAttachment(removed.id).catch(() => {});
      }
    },
    [revoke]
  );

  /** Called after a successful submit: the attachments now belong to the thread. */
  const clear = useCallback(() => {
    current.current.forEach((a) => revoke(a.previewUrl));
    setAttachments([]);
  }, [revoke]);

  /** Attach to the textarea. A non-image paste is left entirely to the browser. */
  const onPaste = useCallback(
    (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
      const files = imageFilesFrom(e.clipboardData);
      if (files.length === 0) return;
      // A screenshot paste also carries a text/plain filename. Left to the
      // browser, that filename is what lands in the textarea — the exact
      // degradation this handler exists to prevent.
      e.preventDefault();
      addFiles(files);
    },
    [addFiles]
  );

  /**
   * Spread onto the composer's wrapper. `preventDefault` is conditional on the
   * drag carrying files, so dropping selected text into the textarea still
   * behaves the way the browser intends.
   */
  const dropZoneProps = {
    onDragOver: (e: React.DragEvent<HTMLElement>) => {
      if (!dragHasFiles(e.dataTransfer?.types)) return;
      e.preventDefault();
      setDragging(true);
    },
    onDragLeave: () => setDragging(false),
    onDrop: (e: React.DragEvent<HTMLElement>) => {
      if (!dragHasFiles(e.dataTransfer?.types)) return;
      e.preventDefault();
      setDragging(false);
      addFiles(imageFilesFrom(e.dataTransfer));
    },
  };

  const uploaded: UploadedAttachmentRef[] = attachments
    .filter((a) => a.status === "done" && a.id !== undefined)
    .map((a) => ({ id: a.id as number, filename: a.filename }));

  return {
    attachments,
    uploaded,
    uploading: attachments.some((a) => a.status === "uploading"),
    dragging,
    addFiles,
    remove,
    clear,
    onPaste,
    dropZoneProps,
  };
}
