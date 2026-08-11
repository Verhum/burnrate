"use client";

import { useEffect, useState } from "react";
import { useRunStore } from "@/stores/run-store";
import { useTaskStore } from "@/stores/task-store";
import { Button, ConfirmDialog, Modal, Spinner } from "@/components/ui";
import { client } from "@/lib/api/client";
import { apiErrorMessage } from "@/lib/api/errors";
import { copyToClipboard } from "@/lib/clipboard";
import { formatDuration, formatStartTime, formatTimestamp, isActiveRun } from "@/lib/format";
import { toast } from "@/lib/toast";
import { RunLogViewer } from "./run-log-viewer";
import type { Run } from "@/lib/api/types";

interface RunListProps {
  taskId?: number;
}

function computeDuration(run: Run): string {
  if (!run.started_at) return "-";
  const start = new Date(run.started_at).getTime();
  if (isNaN(start)) return "-";

  if (run.ended_at) {
    const end = new Date(run.ended_at).getTime();
    if (isNaN(end)) return "-";
    return formatDuration(end - start);
  }

  // Active run: show elapsed time
  if (isActiveRun(run.status)) {
    return formatDuration(Date.now() - start);
  }

  return "-";
}

function statusColor(status: string): string {
  if (status === "running" || status === "starting" || status === "resuming") {
    return "text-amber";
  }
  if (status === "done" || status === "succeeded") {
    return "text-sage";
  }
  return "text-muted";
}

export function RunList({ taskId }: RunListProps) {
  const { runs, loading, fetchRuns, cancelRun } = useRunStore();
  const { tasks } = useTaskStore();
  const [logRunId, setLogRunId] = useState<number | null>(null);
  const [cancelRunId, setCancelRunId] = useState<number | null>(null);
  const [copyingResume, setCopyingResume] = useState(false);

  useEffect(() => {
    fetchRuns(taskId);
  }, [taskId, fetchRuns]);

  const taskTitle = (id: number): string => {
    const task = tasks.find((t) => t.id === id);
    return task ? task.title : `Task #${id}`;
  };

  const handleCancel = async (runId: number) => {
    setCancelRunId(null);
    await cancelRun(runId);
    fetchRuns(taskId);
  };

  // The command is built server-side because it depends on the daemon's own
  // config — which account directory the session was written under — not on
  // anything the browser knows.
  const handleCopyResume = async (runId: number) => {
    setCopyingResume(true);
    try {
      const { command } = await client.getRunResume(runId);
      if (!command) {
        toast.info(`Run #${runId} has no session`, "It never reported a session id, so there is nothing to resume.");
        return;
      }
      if (await copyToClipboard(command)) {
        toast.success("Resume command copied", command);
      } else {
        toast.error("Couldn't reach the clipboard", command);
      }
    } catch (err) {
      toast.error(`Couldn't build a resume command for run #${runId}`, apiErrorMessage(err));
    } finally {
      setCopyingResume(false);
    }
  };

  const logRun = logRunId != null ? runs.find((r) => r.id === logRunId) : null;

  if (loading) {
    return (
      <div className="flex justify-center py-8">
        <Spinner />
      </div>
    );
  }

  if (runs.length === 0) {
    return (
      <p className="font-mono text-xs text-muted text-center py-8">
        No runs found
      </p>
    );
  }

  return (
    <>
      <div className="flex flex-col gap-0.5">
        {/* Header row */}
        <div
          className="grid items-center bg-surface px-3 py-1.5"
          style={{ gridTemplateColumns: "50px 1fr 90px 118px 70px 70px 70px 36px" }}
        >
          <span className="text-[9px] font-bold uppercase tracking-widest text-muted">ID</span>
          <span className="text-[9px] font-bold uppercase tracking-widest text-muted">Task</span>
          <span className="text-[9px] font-bold uppercase tracking-widest text-muted">Status</span>
          <span className="text-[9px] font-bold uppercase tracking-widest text-muted">Started</span>
          <span className="text-[9px] font-bold uppercase tracking-widest text-muted text-right">Cost</span>
          <span className="text-[9px] font-bold uppercase tracking-widest text-muted text-right">Turns</span>
          <span className="text-[9px] font-bold uppercase tracking-widest text-muted text-right">Dur</span>
          <span />
        </div>

        {/* Run rows */}
        {runs.map((run) => (
          <div
            key={run.id}
            className="grid items-center bg-surface px-3 py-1.5 hover:bg-elevated cursor-pointer transition-colors"
            style={{ gridTemplateColumns: "50px 1fr 90px 118px 70px 70px 70px 36px" }}
            onClick={() => setLogRunId(run.id)}
          >
            <span className="font-mono text-xs text-muted tabular-nums">{run.id}</span>
            <span className="font-mono text-xs text-primary truncate pr-2">{taskTitle(run.task_id)}</span>
            <span className={`font-mono font-bold text-[10px] uppercase ${statusColor(run.status)}`}>
              {run.status}
            </span>
            <span
              className="font-mono text-xs text-muted tabular-nums whitespace-nowrap truncate pr-2"
              title={formatTimestamp(run.started_at)}
            >
              {formatStartTime(run.started_at)}
            </span>
            <span className="font-mono text-xs text-muted tabular-nums text-right">
              ${run.cost_usd.toFixed(4)}
            </span>
            <span className="font-mono text-xs text-muted tabular-nums text-right">
              {run.num_turns}
            </span>
            <span className="font-mono text-xs text-muted tabular-nums text-right">
              {computeDuration(run)}
            </span>
            <span className="text-right">
              {isActiveRun(run.status) && (
                <button
                  className="text-[9px] font-bold uppercase tracking-wider text-muted hover:text-danger
                    cursor-pointer border-none bg-transparent font-mono px-1 py-0.5 transition-colors"
                  title={`Cancel run #${run.id}`}
                  onClick={(e) => {
                    e.stopPropagation();
                    setCancelRunId(run.id);
                  }}
                >
                  ✕
                </button>
              )}
            </span>
          </div>
        ))}
      </div>

      <Modal
        open={logRunId != null}
        onClose={() => setLogRunId(null)}
        title={
          logRun
            ? logRun.started_at
              ? `Run #${logRun.id} Log — started ${formatStartTime(logRun.started_at)}`
              : `Run #${logRun.id} Log`
            : "Run Log"
        }
      >
        {logRunId != null && (
          <div className="flex flex-col gap-3">
            <div className="flex items-center gap-2">
              {logRun && isActiveRun(logRun.status) && (
                <Button
                  variant="primary"
                  size="sm"
                  onClick={() => setCancelRunId(logRun.id)}
                >
                  Cancel
                </Button>
              )}
              {logRun?.session_id && (
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={copyingResume}
                  onClick={() => handleCopyResume(logRun.id)}
                  title={`Copy the shell command that reattaches to session ${logRun.session_id}`}
                >
                  {copyingResume ? "Copying…" : "Copy resume cmd"}
                </Button>
              )}
              {logRun?.pr_url && (
                <a
                  href={logRun.pr_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="font-mono text-[9px] font-bold uppercase tracking-wider text-amber hover:text-gold"
                >
                  View PR
                </a>
              )}
            </div>
            <RunLogViewer
              runId={logRunId}
              live={logRun ? isActiveRun(logRun.status) : false}
            />
          </div>
        )}
      </Modal>

      {/* After the log Modal in DOM order so it stacks above it; Modal's
          Escape handling is topmost-only, so cancelling here leaves the log
          dialog open. */}
      <ConfirmDialog
        open={cancelRunId != null}
        title="Cancel run"
        message={`Kill run #${cancelRunId} now? The agent stops mid-work; the task stays resumable if it recorded a session.`}
        confirmLabel="Cancel run"
        cancelLabel="Keep running"
        destructive
        onConfirm={() => {
          if (cancelRunId != null) handleCancel(cancelRunId);
        }}
        onCancel={() => setCancelRunId(null)}
      />
    </>
  );
}
