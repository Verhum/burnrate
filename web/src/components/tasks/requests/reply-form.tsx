"use client";

import { useState } from "react";
import type { HumanRequest, RequestResult } from "@/lib/api/types";
import { client } from "@/lib/api/client";
import { apiErrorMessage } from "@/lib/api/errors";
import { composeBodyWithAttachments } from "@/lib/composer-attachments";
import { useComposerAttachments } from "@/hooks/use-composer-attachments";
import { ComposerAttachmentRow } from "@/components/attachments/composer-attachment-row";
import { toast } from "@/lib/toast";
import { useRequestStore } from "@/stores/request-store";
import { useTaskStore } from "@/stores/task-store";
import { ResultToggle } from "./result-toggle";

interface RequestReplyFormProps {
  request: HumanRequest;
  /**
   * Called after the response lands, so the comment thread can refetch.
   * Optional: the banner has no thread to refresh, and the store refresh below
   * is what clears the answered request from both surfaces.
   */
  onResponded?: () => void;
}

/**
 * The human's answer to a `question` or `demo` request. Multi-line, because the
 * body is markdown and lands verbatim in the task thread; ⌘/Ctrl+Enter submits.
 *
 * Screenshots can be pasted or dropped straight in: each image uploads to the
 * task's attachments immediately and is appended to the submitted body as a
 * markdown image line, so it renders inline in the thread *and* reaches the
 * agent's next run through the runner's `## Image Attachments` section.
 */
export function RequestReplyForm({ request, onResponded }: RequestReplyFormProps) {
  const isDemo = request.kind === "demo";
  const [body, setBody] = useState("");
  const [result, setResult] = useState<RequestResult | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const {
    attachments,
    uploaded,
    uploading,
    dragging,
    remove,
    clear,
    onPaste,
    dropZoneProps,
  } = useComposerAttachments(request.task_id);

  const fetchRequests = useRequestStore((s) => s.fetchRequests);
  const fetchTasks = useTaskStore((s) => s.fetchTasks);

  // A demo verdict is an answer on its own — "pass" with no prose is the common
  // case. A question needs words, or at least a screenshot.
  const canSubmit =
    body.trim() !== "" || uploaded.length > 0 || (isDemo && result !== null);

  const handleSubmit = async () => {
    if (!canSubmit || submitting) return;
    setSubmitting(true);
    try {
      await client.respondToRequest(request.id, {
        body: composeBodyWithAttachments(body, uploaded),
        ...(isDemo && result ? { result } : {}),
      });
      setBody("");
      setResult(null);
      clear();
      onResponded?.();
      await Promise.all([fetchRequests(), fetchTasks()]);
    } catch (err) {
      toast.error("Reply failed", apiErrorMessage(err));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="flex flex-col gap-1.5" {...dropZoneProps}>
      <ComposerAttachmentRow
        attachments={attachments}
        onRemove={remove}
        disabled={submitting}
      />
      <textarea
        value={body}
        onChange={(e) => setBody(e.target.value)}
        onPaste={onPaste}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
            e.preventDefault();
            handleSubmit();
          }
        }}
        placeholder={
          isDemo
            ? "What happened? Markdown, as long as you like. Paste a screenshot to attach it."
            : "Type your reply… (markdown, paste a screenshot, ⌘↵ to send)"
        }
        className={`bg-surface text-[11px] text-primary font-mono px-2 py-1.5 border-none outline-none
          resize-y min-h-[64px] placeholder:text-muted focus:bg-raised transition-colors
          ${dragging ? "shadow-[inset_0_0_0_1px_var(--color-amber)]" : ""}`}
      />
      <div className="flex items-center gap-1.5">
        {isDemo && (
          <ResultToggle value={result} onChange={setResult} disabled={submitting} />
        )}
        <div className="flex-1" />
        {uploading && (
          <span className="text-[9px] text-muted font-mono">uploading…</span>
        )}
        {isDemo && (
          <span className="text-[9px] text-muted font-mono">
            screen recording lands in a later milestone
          </span>
        )}
        <button
          className="text-[9px] font-bold uppercase tracking-wider text-surface bg-amber px-2.5 py-1
            hover:bg-amber/80 transition-colors cursor-pointer border-none font-mono
            disabled:opacity-40 disabled:cursor-not-allowed"
          disabled={!canSubmit || submitting}
          onClick={handleSubmit}
        >
          {submitting ? "Sending…" : isDemo ? "Submit" : "Reply"}
        </button>
      </div>
    </div>
  );
}
