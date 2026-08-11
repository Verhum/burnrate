"use client";

import { useState } from "react";
import type { Task, ForecastEntry, TaskStats, Run, RunningRun } from "@/lib/api/types";
import { Badge, Button, ConfirmDialog } from "@/components/ui";
import { useTaskStore } from "@/stores/task-store";
import { useRunStore } from "@/stores/run-store";
import { useUsageStore } from "@/stores/usage-store";
import { useConfigStore } from "@/stores/config-store";
import { client } from "@/lib/api/client";
import { isActiveRun } from "@/lib/format";
import { apiErrorMessage } from "@/lib/api/errors";
import { toast } from "@/lib/toast";
import { taskPRs, prLabel, prStateColor, prStateTitle, prVisualState } from "@/lib/task-prs";
import { RunLogModal } from "@/components/runs/run-log-modal";
import { ForecastChip } from "./forecast-chip";
import { TaskStatusControl } from "./task-status-control";
import { formatTaskCost, formatTaskDuration, formatLines } from "@/lib/task-stats";

interface TaskCardProps {
  task: Task;
  forecast?: ForecastEntry;
  selected?: boolean;
  onSelect?: (id: number) => void;
  onEdit?: (id: number) => void;
}

// Stable identity for the "no runs yet" case. Returning a fresh `[]` from a
// zustand selector makes every snapshot compare unequal under Object.is, which
// re-renders forever (React #185) until `status` first loads.
const NO_RUNNING_RUNS: RunningRun[] = [];

function stripeColor(status: string): string {
  switch (status) {
    case "running":
    case "queued":
    case "resumable":
      return "bg-dim";
    case "done":
      return "bg-sage";
    default:
      return "bg-muted";
  }
}

export function TaskCard({
  task,
  forecast,
  selected,
  onSelect,
  onEdit,
}: TaskCardProps) {
  const { deleteTask, runNow } = useTaskStore();
  const { cancelActiveRunForTask } = useRunStore();
  const runningRuns = useUsageStore((s) => s.status?.running_runs ?? NO_RUNNING_RUNS);
  const launching = useTaskStore((s) => s.launching[task.id] ?? false);
  const stats = useTaskStore((s) => s.stats[task.id]) as TaskStats | undefined;
  const checkedOutTaskId = useConfigStore((s) => s.config?.checked_out_task_id);
  const isCheckedOut = Number(checkedOutTaskId) === task.id;
  const [pending, setPending] = useState<{ kind: "run" | "delete" | "cancel" } | null>(null);
  const [logRun, setLogRun] = useState<Run | null>(null);
  const [checkingOut, setCheckingOut] = useState(false);
  const displayId = task.display_id || `BR${task.id}`;
  const isRunning = task.status === "running";

  const handleRunningClick = async (e: React.MouseEvent) => {
    e.stopPropagation();
    const rr = runningRuns.find((r) => r.task_id === task.id);
    if (rr) {
      const runs = await client.listRuns({ taskId: task.id, limit: 10 });
      const active = runs.find((r) => r.id === rr.run_id) ?? runs.find((r) => isActiveRun(r.status));
      if (active) { setLogRun(active); return; }
    }
    const runs = await client.listRuns({ taskId: task.id, limit: 10 });
    const active = runs.find((r) => isActiveRun(r.status));
    if (active) setLogRun(active);
  };

  const handleCheckout = async (e: React.MouseEvent) => {
    e.stopPropagation();
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

  const canRunNow = task.status === "queued" || task.status === "resumable";
  const prs = taskPRs(task);

  // A running task already shows a "running" badge; repeating it in a chip is
  // noise.
  const fcChip =
    forecast && !isRunning ? <ForecastChip forecast={forecast} /> : null;

  return (
    <div
      className={`flex items-stretch cursor-pointer hover:bg-elevated transition-colors ${
        selected ? "bg-elevated" : "bg-surface"
      }`}
      onClick={() => onSelect?.(task.id)}
    >
      {/* Status stripe */}
      <div
        className={`w-1 ${stripeColor(task.status)} ${
          isRunning ? "animate-pulse" : ""
        }`}
      />

      {/* Drag handle */}
      <span
        className="text-muted cursor-grab select-none text-lg leading-none px-2 flex items-center drag-handle"
        onClick={(e) => e.stopPropagation()}
      >
        =
      </span>

      {/* Task body */}
      <div className="flex-1 min-w-0 py-2 pr-2">
        <div className="flex items-center gap-2">
          <span className="text-[11px] text-muted font-mono">
            {displayId}
          </span>
          <span className="text-[13px] text-primary truncate">{task.title}</span>
        </div>
        <div className="flex items-center gap-1.5 mt-1 flex-wrap">
          <Badge
            variant={task.status as "queued"}
            onClick={isRunning ? handleRunningClick : undefined}
          >
            {task.status}
          </Badge>
          {task.latest_run_status &&
            task.latest_run_status !== task.status &&
            task.status !== "done" &&
            task.status !== "pr_created" && (
            <Badge variant={task.latest_run_status as "running"}>
              {task.latest_run_status}
            </Badge>
          )}
          {isCheckedOut && (
            <span className="text-[9px] font-bold uppercase tracking-wider text-sage bg-sage/15 px-2 py-0.5">
              ✓ Checked out
            </span>
          )}
          {prs.map((pr) => (
            <a
              key={pr.pr_url}
              href={pr.pr_url}
              target="_blank"
              rel="noopener noreferrer"
              title={`${prStateTitle(pr)} — ${pr.pr_url}`}
              className={`text-[9px] font-bold uppercase tracking-wider hover:text-primary ${
                prVisualState(pr) === "unknown"
                  ? task.status === "pr_created"
                    ? "text-amber"
                    : "text-dim"
                  : prStateColor(pr)
              } ${task.status === "pr_created" ? "bg-amber/15 px-2 py-0.5" : ""}`}
              onClick={(e) => e.stopPropagation()}
            >
              {prLabel(pr, task.status === "pr_created" ? "Review PR" : "PR")}
            </a>
          ))}
          {fcChip}
        </div>
        {stats && stats.runs > 0 && (
          <div className="flex items-center gap-3 mt-1 text-[10px] text-muted font-mono">
            <span title="Total cost">{formatTaskCost(stats.cost_usd)}</span>
            <span title="Lines changed">{formatLines(stats.lines_added, stats.lines_removed)}</span>
            <span title="Total wall-clock time">{formatTaskDuration(stats.duration_sec)}</span>
            <span title="Number of runs">{stats.runs} {stats.runs === 1 ? "run" : "runs"}</span>
          </div>
        )}
      </div>

      {/* Actions */}
      <div
        className="flex items-center gap-1 pr-2"
        onClick={(e) => e.stopPropagation()}
      >
        {prs.length > 0 && (
          <Button
            variant="ghost"
            size="sm"
            disabled={checkingOut}
            onClick={handleCheckout}
            title="Check out this task's branches in your local clones"
          >
            {checkingOut ? "…" : "Checkout"}
          </Button>
        )}
        {canRunNow && (
          <Button
            variant="primary"
            size="sm"
            disabled={launching}
            onClick={() => setPending({ kind: "run" })}
          >
            {launching ? "Launching…" : "Run now"}
          </Button>
        )}
        {isRunning && (
          <Button
            variant="primary"
            size="sm"
            onClick={() => setPending({ kind: "cancel" })}
          >
            Cancel
          </Button>
        )}
        {onEdit && (
          <Button variant="ghost" size="sm" onClick={() => onEdit(task.id)}>
            Edit
          </Button>
        )}
        <TaskStatusControl task={task} />
        {!isRunning && (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setPending({ kind: "delete" })}
          >
            Del
          </Button>
        )}

        <RunLogModal run={logRun} onClose={() => setLogRun(null)} />

        <ConfirmDialog
          open={pending !== null}
          title={
            pending?.kind === "cancel"
              ? "Cancel run"
              : pending?.kind === "delete"
                ? "Delete task"
                : "Run now"
          }
          message={
            pending?.kind === "cancel"
              ? `Kill the active run for ${displayId} now? The agent stops mid-work; the task stays resumable if it recorded a session.`
              : pending?.kind === "delete"
                ? `Delete ${displayId} — ${task.title}? Its runs and comments go with it.`
                : `Launch ${displayId} immediately, ahead of the queue?`
          }
          confirmLabel={
            pending?.kind === "cancel"
              ? "Cancel run"
              : pending?.kind === "delete"
                ? "Delete"
                : "Run now"
          }
          cancelLabel={pending?.kind === "cancel" ? "Keep running" : undefined}
          destructive={pending?.kind === "delete" || pending?.kind === "cancel"}
          onConfirm={() => {
            const kind = pending?.kind;
            setPending(null);
            if (kind === "delete") deleteTask(task.id);
            else if (kind === "run") runNow(task.id);
            else if (kind === "cancel") cancelActiveRunForTask(task.id);
          }}
          onCancel={() => setPending(null)}
        />
      </div>
    </div>
  );
}
