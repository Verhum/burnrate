"use client";

import { useState, useEffect, useCallback } from "react";
import type { Task, TaskPR, TaskStats as TaskStatsType, Run } from "@/lib/api/types";
import { Badge, Button, ConfirmDialog } from "@/components/ui";
import { CommentThread } from "@/components/comments/comment-thread";
import { AttachmentGallery } from "@/components/attachments/attachment-gallery";
import { RunList } from "@/components/runs/run-list";
import { RunLogModal } from "@/components/runs/run-log-modal";
import { TaskRequests } from "./requests/task-requests";
import { useTaskStore } from "@/stores/task-store";
import { useRunStore } from "@/stores/run-store";
import { useConfigStore } from "@/stores/config-store";
import { useRequestStore } from "@/stores/request-store";
import { client } from "@/lib/api/client";
import { taskPRs, prLabel, prStateColor, prStateTitle, prVisualState } from "@/lib/task-prs";
import { isActiveRun } from "@/lib/format";
import { apiErrorMessage } from "@/lib/api/errors";
import { toast } from "@/lib/toast";
import { TaskStatusControl } from "./task-status-control";
import { formatTaskCost, formatTaskDuration, formatLines } from "@/lib/task-stats";

const prButtonStyle: Record<string, string> = {
  merged: "bg-merged text-surface hover:bg-merged/80",
  closed: "bg-danger text-surface hover:bg-danger/80",
  draft: "bg-warm text-primary hover:bg-warm/80",
  open: "bg-sage text-surface hover:bg-sage/80",
  unknown: "bg-amber text-surface hover:bg-amber/80",
};

function prButtonClasses(pr: TaskPR): string {
  return prButtonStyle[prVisualState(pr)] || prButtonStyle.unknown;
}

interface TaskDetailProps {
  task: Task;
  onClose: () => void;
  onEdit: (id: number) => void;
}

export function TaskDetail({ task, onClose, onEdit }: TaskDetailProps) {
  const { changeStatus, runNow, fetchTasks } = useTaskStore();
  const launching = useTaskStore((s) => s.launching[task.id] ?? false);
  const stats = useTaskStore((s) => s.stats[task.id]) as TaskStatsType | undefined;
  const { runs, cancelRun, fetchRuns } = useRunStore();
  const checkedOutTaskId = useConfigStore((s) => s.config?.checked_out_task_id);
  const isCheckedOut = Number(checkedOutTaskId) === task.id;
  const [runNowOpen, setRunNowOpen] = useState(false);
  const [cancelOpen, setCancelOpen] = useState(false);
  const displayId = task.display_id || `BR${task.id}`;
  const isRunning = task.status === "running";
  const isReview = task.status === "pr_created";
  const isAwaitingHuman = task.status === "awaiting_human";
  const canRunNow = task.status === "queued" || task.status === "resumable";
  const prs = taskPRs(task);
  const activeRun = isRunning ? runs.find((r) => isActiveRun(r.status)) : null;

  const [logRun, setLogRun] = useState<Run | null>(null);
  const pendingLogRunId = useTaskStore((s) => s.pendingLogRunId);
  const clearPendingLogRunId = useTaskStore((s) => s.clearPendingLogRunId);
  const [bannerComment, setBannerComment] = useState("");
  const [bannerSubmitting, setBannerSubmitting] = useState(false);
  const [checkingOut, setCheckingOut] = useState(false);
  const [refreshing, setRefreshing] = useState(false);

  // A response creates a comment the thread knows nothing about, and a response
  // filed from outside this browser (MCP, another window) does the same. The
  // store's revision covers the second case; the local bump makes the first
  // immediate rather than dependent on the SSE round trip landing.
  const [responseBump, setResponseBump] = useState(0);
  const requestRevision = useRequestStore((s) => s.revision);
  const commentsKey = `${requestRevision}:${responseBump}`;
  const handleResponded = useCallback(() => setResponseBump((n) => n + 1), []);

  useEffect(() => {
    if (!pendingLogRunId) return;
    clearPendingLogRunId();
    (async () => {
      const fetched = await client.listRuns({ taskId: task.id, limit: 10 });
      const target = fetched.find((r) => r.id === pendingLogRunId) ?? fetched.find((r) => isActiveRun(r.status));
      if (target) setLogRun(target);
    })();
  }, [pendingLogRunId, clearPendingLogRunId, task.id]);

  const handleRunningClick = async () => {
    if (activeRun) { setLogRun(activeRun); return; }
    const fetched = await client.listRuns({ taskId: task.id, limit: 10 });
    const active = fetched.find((r) => isActiveRun(r.status));
    if (active) setLogRun(active);
  };

  // Checks the task's branches out in the user's own clones, so a single local
  // server can be pointed at the work without cloning per-PR worktrees.
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

  const handleRefreshPRs = async () => {
    setRefreshing(true);
    try {
      await client.refreshTaskPRs(task.id);
      await fetchTasks();
    } catch (err) {
      toast.error("Couldn't refresh PR status", apiErrorMessage(err));
    } finally {
      setRefreshing(false);
    }
  };

  const handleBannerDone = async () => {
    if (bannerComment.trim()) {
      setBannerSubmitting(true);
      try {
        await client.createComment(task.id, { body: bannerComment.trim() });
      } catch {
        // ignore
      }
      setBannerSubmitting(false);
    }
    changeStatus(task.id, "done");
  };

  const handleBannerSubmitComment = async () => {
    if (!bannerComment.trim() || bannerSubmitting) return;
    setBannerSubmitting(true);
    try {
      await client.createComment(task.id, { body: bannerComment.trim() });
      setBannerComment("");
      await fetchTasks();
    } catch {
      // ignore
    } finally {
      setBannerSubmitting(false);
    }
  };

  return (
    <div className="flex flex-col gap-0.5">
      {isReview && (
        <div className="bg-rust px-6 py-3">
          <div className="flex items-center gap-3 mb-2">
            <span className="text-[9px] font-bold uppercase tracking-widest text-black/60">
              Ready for Review
            </span>
            <div className="flex-1" />
            {prs.map((pr) => (
              <a
                key={pr.pr_url}
                href={pr.pr_url}
                target="_blank"
                rel="noopener noreferrer"
                title={`${prStateTitle(pr)} — ${pr.pr_url}`}
                className={`inline-flex items-center gap-1.5 text-xs font-bold uppercase tracking-wider bg-black/80 px-4 py-1.5 hover:bg-black transition-colors ${prStateColor(pr)}`}
              >
                {prLabel(pr, "View PR")}
                <span className="text-[10px] opacity-60">↗</span>
              </a>
            ))}
            <button
              className="inline-flex items-center text-[9px] font-bold uppercase tracking-wider text-black/70 bg-black/20 px-2.5 py-1 hover:bg-black/30 transition-colors cursor-pointer border-none font-mono disabled:opacity-40 disabled:cursor-not-allowed"
              disabled={bannerSubmitting}
              onClick={handleBannerDone}
            >
              Done
            </button>
          </div>
          <div className="flex items-center gap-1.5">
            <input
              type="text"
              value={bannerComment}
              onChange={(e) => setBannerComment(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.shiftKey) {
                  e.preventDefault();
                  handleBannerSubmitComment();
                }
              }}
              placeholder="Leave a comment..."
              className="flex-1 bg-black/10 text-[11px] text-black font-mono px-2 py-1 placeholder:text-black/50 focus:bg-black/15 focus:outline-none border-none"
            />
            {bannerComment.trim() && (
              <button
                className="text-[9px] font-bold uppercase tracking-wider text-black/70 bg-black/20 px-2 py-1 hover:bg-black/30 transition-colors cursor-pointer border-none font-mono disabled:opacity-40"
                disabled={bannerSubmitting}
                onClick={handleBannerSubmitComment}
              >
                Send
              </button>
            )}
          </div>
        </div>
      )}

      <TaskRequests
        taskId={task.id}
        awaitingHuman={isAwaitingHuman}
        onResponded={handleResponded}
      />

      <div className="bg-surface px-6 py-3 flex items-center gap-3">
        <button
          onClick={onClose}
          className="px-3 py-1.5 bg-raised text-dim hover:bg-warm hover:text-primary
            text-[10px] font-bold uppercase tracking-wider cursor-pointer border-none font-mono transition-colors"
        >
          ← BACK
        </button>
        <span className="text-[10px] text-muted font-bold">{displayId}</span>
        <span className="text-sm font-bold text-primary flex-1 truncate">
          {task.title}
        </span>
        {prs.length > 0 && prs.map((pr) => (
          <a
            key={pr.pr_url}
            href={pr.pr_url}
            target="_blank"
            rel="noopener noreferrer"
            title={`${prStateTitle(pr)} — ${pr.pr_url}`}
            className={`text-[10px] font-bold uppercase tracking-wider no-underline shrink-0 ${prStateColor(pr)} hover:text-primary`}
          >
            {prLabel(pr, "PR")}
          </a>
        ))}
        {isCheckedOut && (
          <span className="text-[9px] font-bold uppercase tracking-wider text-sage bg-sage/15 px-2 py-0.5">
            ✓ Checked out
          </span>
        )}
        <Badge
          variant={task.status as "queued"}
          onClick={isRunning ? handleRunningClick : undefined}
        >
          {task.status}
        </Badge>
        <div className="flex items-center gap-1.5 shrink-0">
          {canRunNow && (
            <Button
              variant="accent"
              size="sm"
              disabled={launching}
              onClick={() => setRunNowOpen(true)}
            >
              {launching ? "Launching…" : "Run now"}
            </Button>
          )}
          {isRunning && activeRun && (
            <Button
              variant="primary"
              size="sm"
              onClick={() => setCancelOpen(true)}
            >
              Cancel run
            </Button>
          )}
          <TaskStatusControl task={task} quickActions={!isReview} />
          <Button size="sm" onClick={() => onEdit(task.id)}>
            Edit
          </Button>
        </div>
      </div>

      {!isReview && prs.length > 0 && (
        <div className="bg-elevated px-6 py-3 flex items-center gap-3">
          {prs.map((pr) => (
            <a
              key={pr.pr_url}
              href={pr.pr_url}
              target="_blank"
              rel="noopener noreferrer"
              title={`${prStateTitle(pr)} — ${pr.pr_url}`}
              className={`flex-1 inline-flex items-center justify-center gap-2 font-mono font-bold uppercase tracking-wider
                text-sm px-5 py-3 no-underline transition-colors ${prButtonClasses(pr)}`}
            >
              {prLabel(pr, "Open PR")}
              <span className="text-xs opacity-60">↗</span>
            </a>
          ))}
        </div>
      )}

      {task.summary && (
        <div className="bg-surface px-6 py-3">
          <div className="text-[9px] font-bold uppercase tracking-widest text-muted mb-1">
            SUMMARY
          </div>
          <div className="text-xs text-primary">{task.summary}</div>
        </div>
      )}

      {task.prompt && (
        <div className="bg-surface px-6 py-4">
          <div className="text-[9px] font-bold uppercase tracking-widest text-muted mb-2">
            PROMPT
          </div>
          <pre className="text-xs text-primary whitespace-pre-wrap font-mono">
            {task.prompt}
          </pre>
        </div>
      )}

      <div className="bg-surface px-6 py-3 flex gap-6 text-xs">
        {task.repo_path && (
          <div>
            <span className="text-muted">Repo </span>
            <span className="text-dim font-mono text-[11px]">
              {task.repo_path}
            </span>
          </div>
        )}
        <div>
          <span className="text-muted">Session </span>
          <span className="text-dim">{task.has_session ? "yes" : "no"}</span>
        </div>
      </div>

      {stats && stats.runs > 0 && (
        <div className="bg-surface px-6 py-3">
          <div className="text-[9px] font-bold uppercase tracking-widest text-muted mb-2">
            STATS
          </div>
          <div className="flex gap-6 text-xs">
            <div>
              <span className="text-muted">Cost </span>
              <span className="text-primary font-mono">{formatTaskCost(stats.cost_usd)}</span>
            </div>
            <div>
              <span className="text-muted">Lines </span>
              <span className="text-primary font-mono">{formatLines(stats.lines_added, stats.lines_removed)}</span>
            </div>
            <div>
              <span className="text-muted">Time </span>
              <span className="text-primary font-mono">{formatTaskDuration(stats.duration_sec)}</span>
            </div>
            <div>
              <span className="text-muted">Runs </span>
              <span className="text-primary font-mono">{stats.runs}</span>
            </div>
            {stats.model && (
              <div>
                <span className="text-muted">Model </span>
                <span className="text-dim font-mono text-[11px]">{stats.model}</span>
              </div>
            )}
          </div>
        </div>
      )}

      {prs.length > 0 && (
        <div className="bg-surface px-6 py-4">
          <div className="flex items-center gap-3 mb-3">
            <div className="text-[9px] font-bold uppercase tracking-widest text-muted">
              PULL REQUESTS
            </div>
            <div className="flex-1" />
            <Button
              size="sm"
              disabled={refreshing}
              onClick={handleRefreshPRs}
              title="Ask gh for each PR's current state"
            >
              {refreshing ? "Probing…" : "Refresh status"}
            </Button>
            <Button
              size="sm"
              disabled={checkingOut}
              onClick={handleCheckout}
              title="Check these branches out in your local clones under base_code_dir"
            >
              {checkingOut ? "Checking out…" : "Checkout"}
            </Button>
          </div>
          <div className="flex flex-col gap-1.5">
            {prs.map((pr) => (
              <a
                key={`${pr.id}-${pr.pr_url}`}
                href={pr.pr_url}
                target="_blank"
                rel="noopener noreferrer"
                title={`${prStateTitle(pr)} — ${pr.pr_url}`}
                className="flex items-center gap-3 px-3 py-2 no-underline transition-colors hover:bg-elevated"
              >
                <span className="text-dim font-mono text-[11px] min-w-[180px] truncate">
                  {pr.repo || "—"}
                </span>
                <span className="text-muted font-mono text-[11px] flex-1 truncate">
                  {pr.branch || "—"}
                </span>
                <span
                  className={`text-[9px] font-bold uppercase tracking-wider ${prStateColor(pr)}`}
                >
                  {prVisualState(pr) === "unknown" ? "—" : prVisualState(pr)}
                </span>
                <span className={`text-[10px] font-bold uppercase tracking-wider font-mono ${prStateColor(pr)}`}>
                  {prLabel(pr, "View PR")} ↗
                </span>
              </a>
            ))}
          </div>
        </div>
      )}

      <div className="bg-surface px-6 py-4">
        <div className="text-[9px] font-bold uppercase tracking-widest text-muted mb-3">
          RUNS
        </div>
        <RunList taskId={task.id} />
      </div>

      <div className="bg-surface px-6 py-4">
        <div className="text-[9px] font-bold uppercase tracking-widest text-muted mb-3">
          COMMENTS
        </div>
        <CommentThread taskId={task.id} isRunning={isRunning} refreshKey={commentsKey} />
      </div>

      {prs.length > 0 && (
        <div className="bg-elevated px-6 py-5 flex flex-col items-center gap-3">
          <div className="flex items-center gap-3 w-full">
            {prs.map((pr) => (
              <a
                key={pr.pr_url}
                href={pr.pr_url}
                target="_blank"
                rel="noopener noreferrer"
                title={`${prStateTitle(pr)} — ${pr.pr_url}`}
                className={`flex-1 inline-flex items-center justify-center gap-2 font-mono font-bold uppercase tracking-wider
                  text-sm px-5 py-3 no-underline transition-colors ${prButtonClasses(pr)}`}
              >
                {prLabel(pr, "Review PR")}
                <span className="text-xs opacity-60">↗</span>
              </a>
            ))}
          </div>
          <div className="flex items-center gap-3">
            <Button
              size="sm"
              disabled={refreshing}
              onClick={handleRefreshPRs}
            >
              {refreshing ? "Probing…" : "Refresh status"}
            </Button>
            <Button
              size="sm"
              disabled={checkingOut}
              onClick={handleCheckout}
            >
              {checkingOut ? "Checking out…" : "Checkout"}
            </Button>
          </div>
        </div>
      )}

      <div className="bg-surface px-6 py-4">
        <div className="text-[9px] font-bold uppercase tracking-widest text-muted mb-3">
          ATTACHMENTS
        </div>
        <AttachmentGallery taskId={task.id} />
      </div>

      <RunLogModal run={logRun} onClose={() => setLogRun(null)} />

      <ConfirmDialog
        open={runNowOpen}
        title="Run now"
        message={`Launch ${displayId} immediately, ahead of the queue?`}
        confirmLabel="Run now"
        onConfirm={() => {
          setRunNowOpen(false);
          runNow(task.id);
        }}
        onCancel={() => setRunNowOpen(false)}
      />

      <ConfirmDialog
        open={cancelOpen}
        title="Cancel run"
        message={`Kill run #${activeRun?.id} now? The agent stops mid-work; the task stays resumable if it recorded a session.`}
        confirmLabel="Cancel run"
        cancelLabel="Keep running"
        destructive
        onConfirm={async () => {
          setCancelOpen(false);
          if (activeRun) {
            await cancelRun(activeRun.id);
            fetchRuns(task.id);
          }
        }}
        onCancel={() => setCancelOpen(false)}
      />
    </div>
  );
}
