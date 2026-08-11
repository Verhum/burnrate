"use client";

import { useState, useEffect } from "react";
import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";
import type { Task, Comment } from "@/lib/api/types";
import { useTaskStore } from "@/stores/task-store";
import { useConfigStore } from "@/stores/config-store";
import { client } from "@/lib/api/client";
import { apiErrorMessage } from "@/lib/api/errors";
import { toast } from "@/lib/toast";
import { taskPRs, prLabel, prStateColor, prStateTitle } from "@/lib/task-prs";

interface ReviewQueueItemProps {
  task: Task;
  onSelect: (id: number) => void;
}

export function ReviewQueueItem({ task, onSelect }: ReviewQueueItemProps) {
  const { changeStatus, fetchTasks } = useTaskStore();
  const checkedOutTaskId = useConfigStore((s) => s.config?.checked_out_task_id);
  const isCheckedOut = Number(checkedOutTaskId) === task.id;
  const [comment, setComment] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [checkingOut, setCheckingOut] = useState(false);
  const [latestAgentComment, setLatestAgentComment] = useState<Comment | null>(
    null,
  );
  const [expanded, setExpanded] = useState(false);
  const displayId = task.display_id || `BR${task.id}`;
  const prs = taskPRs(task);

  const handleCheckout = async () => {
    setCheckingOut(true);
    try {
      const results = await client.checkoutTask(task.id);
      const failed = results.filter((r) => r.status === "error");
      const ok = results.filter(
        (r) => r.status === "checked_out" || r.status === "already"
      );
      if (failed.length > 0) {
        toast.error(
          `Checked out ${ok.length}/${results.length}`,
          failed.map((r) => `${r.repo}: ${r.detail}`).join("\n")
        );
      } else if (results.length === 0) {
        toast.info("Nothing to check out", "This task has no recorded branches.");
      } else {
        toast.success(
          `Checked out ${ok.length} ${ok.length === 1 ? "branch" : "branches"}`,
          results.map((r) => `${r.repo} → ${r.branch}`).join("\n")
        );
      }
      await useConfigStore.getState().fetchConfig();
    } catch (err) {
      toast.error("Checkout failed", apiErrorMessage(err));
    } finally {
      setCheckingOut(false);
    }
  };

  useEffect(() => {
    client
      .listComments(task.id)
      .then((comments) => {
        const agentComments = comments.filter((c) => c.author === "agent");
        if (agentComments.length > 0) {
          setLatestAgentComment(agentComments[agentComments.length - 1]);
        }
      })
      .catch(() => {});
  }, [task.id]);

  const handleDone = async () => {
    if (comment.trim()) {
      setSubmitting(true);
      try {
        await client.createComment(task.id, { body: comment.trim() });
      } catch {
        // ignore
      }
      setSubmitting(false);
    }
    changeStatus(task.id, "done");
  };

  const handleSubmitComment = async () => {
    if (!comment.trim() || submitting) return;
    setSubmitting(true);
    try {
      await client.createComment(task.id, { body: comment.trim() });
      setComment("");
      await fetchTasks();
    } catch {
      // ignore
    } finally {
      setSubmitting(false);
    }
  };

  const agentBody = latestAgentComment?.body ?? "";
  // Collapse by height, never by slicing: cutting the string mid-Markdown
  // leaves unbalanced fences and links that render as literal syntax.
  const isLong = agentBody.length > 280;

  return (
    <div className="px-3 py-2 border-l-2 my-3 border-black/20">
      <div className="flex items-center gap-3">
        <div
          className="flex-1 min-w-0 cursor-pointer"
          onClick={() => onSelect(task.id)}
        >
          <div className="flex items-center gap-2">
            <span className="text-[11px] text-black/60 font-mono font-bold">
              {displayId}
            </span>
            <span className="text-[13px] text-black font-semibold truncate">
              {task.title}
            </span>
            {isCheckedOut && (
              <span className="text-[9px] font-bold uppercase tracking-wider text-sage bg-sage/25 px-2 py-0.5">
                ✓ Checked out
              </span>
            )}
          </div>
        </div>

        <div
          className="flex items-center gap-1.5"
          onClick={(e) => e.stopPropagation()}
        >
          {prs.length > 0 ? (
            prs.map((pr) => (
              <a
                key={pr.pr_url}
                href={pr.pr_url}
                target="_blank"
                rel="noopener noreferrer"
                title={`${prStateTitle(pr)} — ${pr.pr_url}`}
                className={`inline-flex items-center text-[9px] font-bold uppercase tracking-wider bg-black/80 px-2.5 py-1 hover:bg-black transition-colors ${prStateColor(pr)}`}
              >
                {prLabel(pr, "View PR")}
              </a>
            ))
          ) : (
            <span className="inline-flex items-center text-[9px] font-bold uppercase tracking-wider text-black/60 bg-black/10 px-2.5 py-1">
              No PR
            </span>
          )}
          {prs.length > 0 && (
            <button
              className="inline-flex items-center text-[9px] font-bold uppercase tracking-wider text-black/70 bg-black/20 px-2.5 py-1 hover:bg-black/30 transition-colors cursor-pointer border-none font-mono disabled:opacity-40 disabled:cursor-not-allowed"
              disabled={checkingOut}
              onClick={handleCheckout}
              title="Check out this task's branches in your local clones"
            >
              {checkingOut ? "…" : "Checkout"}
            </button>
          )}
          <button
            className="inline-flex items-center text-[9px] font-bold uppercase tracking-wider text-black/70 bg-black/20 px-2.5 py-1 hover:bg-black/30 transition-colors cursor-pointer border-none font-mono disabled:opacity-40 disabled:cursor-not-allowed"
            disabled={submitting}
            onClick={handleDone}
          >
            Done
          </button>
        </div>
      </div>

      {latestAgentComment && (
        <div className="mt-1.5 bg-surface text-primary px-2.5 py-2">
          <div className="flex items-center gap-1.5 mb-1">
            <span className="text-[9px] font-bold uppercase tracking-widest text-muted">
              Agent
            </span>
          </div>
          <div
            className={`text-[11px] prose-comment ${
              !expanded && isLong ? "max-h-[9em] overflow-hidden" : ""
            }`}
          >
            <Markdown remarkPlugins={[remarkGfm]}>{agentBody}</Markdown>
          </div>
          {isLong && (
            <button
              className="text-[9px] font-bold uppercase tracking-wider text-dim hover:text-primary mt-1 cursor-pointer border-none bg-transparent font-mono"
              onClick={() => setExpanded(!expanded)}
            >
              {expanded ? "Show less" : "Show more"}
            </button>
          )}
        </div>
      )}

      <div className="flex items-center gap-1.5 mt-1.5">
        <input
          type="text"
          value={comment}
          onChange={(e) => setComment(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              handleSubmitComment();
            }
          }}
          placeholder="Leave a comment..."
          className="flex-1 bg-black/10 text-[11px] text-black font-mono px-2 py-1 placeholder:text-black/50 focus:bg-black/15 focus:outline-none border-none"
        />
        {comment.trim() && (
          <button
            className="text-[9px] font-bold uppercase tracking-wider text-black/70 bg-black/20 px-2 py-1 hover:bg-black/30 transition-colors cursor-pointer border-none font-mono disabled:opacity-40"
            disabled={submitting}
            onClick={handleSubmitComment}
          >
            Send
          </button>
        )}
      </div>
    </div>
  );
}
