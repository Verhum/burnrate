"use client";

import { useState, useEffect, useRef, useCallback } from "react";
import type { Task, ForecastEntry } from "@/lib/api/types";
import { Button } from "@/components/ui";
import { useTaskStore } from "@/stores/task-store";
import { useUsageStore } from "@/stores/usage-store";
import { formatDuration, formatStartTime, formatTimestamp } from "@/lib/format";
import { useNow } from "@/hooks/use-now";
import { TaskCard } from "./task-card";
import { TaskForm } from "./task-form";
import { TaskDetail } from "./task-detail";
import { ReviewQueueItem } from "./review-queue-item";
import { PendingRequestsBanner } from "./requests/pending-requests-banner";

const FILTERS = [
  { key: "active", label: "Active" },
  { key: "backlog", label: "Backlog" },
  { key: "completed", label: "Completed" },
  { key: "dismissed", label: "Dismissed" },
  { key: "failed", label: "Failed" },
  { key: "all", label: "All" },
] as const;

function matchesFilter(task: Task, filter: string): boolean {
  switch (filter) {
    case "active":
      return (
        task.status === "queued" ||
        task.status === "running" ||
        task.status === "resumable" ||
        task.status === "paused" ||
        task.status === "awaiting_human"
      );
    case "backlog":
      return task.status === "backlog";
    case "completed":
      return task.status === "done";
    case "dismissed":
      return task.status === "dismissed";
    case "failed":
      return task.status === "failed";
    case "all":
      return true;
    default:
      return true;
  }
}

const EMPTY_MESSAGES: Record<string, string> = {
  active: "No active tasks",
  backlog: "No backlog tasks",
  completed: "No completed tasks",
  dismissed: "No dismissed tasks",
  failed: "No failed tasks",
  all: "No tasks yet",
};

export function TaskList({ addRequested, focusIndex = -1 }: { addRequested?: number; focusIndex?: number }) {
  const {
    tasks,
    filter,
    setFilter,
    selectedTask,
    selectTask,
    fetchTasks,
    reorderTasks,
  } = useTaskStore();
  const status = useUsageStore((s) => s.status);
  // Ticking clock rather than a bare `Date.now()` during render: the active
  // agents' elapsed times are derived, so they now also count up.
  const now = useNow(1000);

  const [searchQuery, setSearchQuery] = useState("");
  const [formOpen, setFormOpen] = useState(false);
  const [editingTask, setEditingTask] = useState<Task | null>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const prevAddRequested = useRef(addRequested ?? 0);

  useEffect(() => {
    fetchTasks();
  }, [fetchTasks]);

  useEffect(() => {
    if (addRequested && addRequested > prevAddRequested.current) {
      setEditingTask(null);
      setFormOpen(true);
    }
    prevAddRequested.current = addRequested ?? 0;
  }, [addRequested]);

  const forecastMap = new Map<number, ForecastEntry>();
  if (status?.forecast) {
    for (const f of status.forecast) {
      forecastMap.set(f.task_id, f);
    }
  }

  const reviewTasks = tasks.filter((t) => t.status === "pr_created");
  const runningRuns = status?.running_runs ?? [];

  const filtered = tasks
    .filter((t) => {
      if (!matchesFilter(t, filter)) return false;
      if (!searchQuery) return true;
      const q = searchQuery.toLowerCase();
      if (t.display_id?.toLowerCase().includes(q)) return true;
      if (/^\d+$/.test(q) && t.id === parseInt(q, 10)) return true;
      return (
        t.title?.toLowerCase().includes(q) ||
        t.prompt?.toLowerCase().includes(q)
      );
    })
    .sort((a, b) => {
      if (filter === "active" || filter === "backlog") return 0;
      const aTime = a.created_at ? new Date(a.created_at).getTime() : 0;
      const bTime = b.created_at ? new Date(b.created_at).getTime() : 0;
      return bTime - aTime;
    });

  const handleSelect = useCallback(
    (id: number) => {
      if (selectedTask?.id === id) {
        selectTask(null);
      } else {
        const t = tasks.find((x) => x.id === id);
        selectTask(t || null);
      }
    },
    [selectedTask, tasks, selectTask]
  );

  const handleEdit = useCallback(
    (id: number) => {
      const t = tasks.find((x) => x.id === id);
      if (t) {
        setEditingTask(t);
        setFormOpen(true);
      }
    },
    [tasks]
  );

  const handleDragEnd = useCallback(() => {
    if (!listRef.current) return;
    const ids = Array.from(listRef.current.children)
      .map((el) => parseInt((el as HTMLElement).dataset.id || "", 10))
      .filter((id) => !isNaN(id));
    if (ids.length) reorderTasks(ids);
  }, [reorderTasks]);

  if (selectedTask) {
    return (
      <TaskDetail
        task={selectedTask}
        onClose={() => selectTask(null)}
        onEdit={handleEdit}
      />
    );
  }

  return (
    <div className="flex flex-col gap-0.5">
      {/* PENDING HUMAN REQUESTS */}
      <PendingRequestsBanner onSelect={handleSelect} />

      {/* REVIEW QUEUE */}
      {reviewTasks.length > 0 && (
        <div className="bg-rust">
          <div className="px-3 py-1.5">
            <span className="text-[9px] font-bold uppercase tracking-widest text-black/60">
              Review Queue ({reviewTasks.length})
            </span>
          </div>
          {reviewTasks.map((t) => (
            <ReviewQueueItem
              key={t.id}
              task={t}
              onSelect={handleSelect}
            />
          ))}
        </div>
      )}

      {/* ACTIVE AGENTS */}
      {runningRuns.length > 0 && (
        <div className="bg-surface px-3 py-2">
          <div className="mb-1.5">
            <span className="text-[9px] font-bold uppercase tracking-widest text-muted">
              Active Agents ({runningRuns.length})
            </span>
          </div>
          <div className="flex flex-wrap gap-1.5">
            {runningRuns.map((run) => (
              <div
                key={run.run_id}
                className="flex items-center gap-1.5 bg-elevated px-2 py-1 cursor-pointer hover:brightness-125 transition-colors"
                onClick={() => {
                  const t = tasks.find((x) => x.id === run.task_id);
                  if (t) selectTask(t, run.run_id);
                }}
              >
                <span className="w-1.5 h-1.5 bg-amber animate-pulse" />
                <span className="text-[11px] text-muted font-mono">
                  BR{run.task_id}
                </span>
                <span className="text-[11px] text-dim truncate max-w-[120px]">
                  {run.title}
                </span>
                <span
                  className="text-[9px] text-muted font-mono tabular-nums"
                  title={`started ${formatTimestamp(run.started_at)}`}
                >
                  {formatDuration(now - new Date(run.started_at).getTime())}
                  <span className="text-dim"> @ {formatStartTime(run.started_at)}</span>
                </span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* TASK CONTROLS */}
      <div className="bg-surface px-3 py-2 flex items-center gap-2" data-tour="add-task">
        <Button
          variant="accent"
          size="sm"
          onClick={() => {
            setEditingTask(null);
            setFormOpen(true);
          }}
        >
          + Add
        </Button>
        <div className="flex-1" />
        {FILTERS.map((f) => (
          <button
            key={f.key}
            className={`px-2 py-0.5 text-[9px] font-bold uppercase tracking-wider font-mono cursor-pointer border-none transition-colors ${
              filter === f.key
                ? "bg-elevated text-dim"
                : "bg-transparent text-muted hover:text-dim"
            }`}
            onClick={() => setFilter(f.key)}
          >
            {f.label}
          </button>
        ))}
      </div>

      {/* TASK LIST */}
      <div
        ref={listRef}
        onDragEnd={handleDragEnd}
        className="flex flex-col gap-0.5"
        data-tour="task-list"
      >
        {filtered.length === 0 ? (
          <div className="bg-surface p-8 text-center text-[13px] text-muted">
            {searchQuery
              ? `No tasks match "${searchQuery}"`
              : EMPTY_MESSAGES[filter] || "No tasks yet"}
          </div>
        ) : (
          filtered.map((t) => (
            <div key={t.id} data-id={t.id} className={filtered.indexOf(t) === focusIndex ? "outline outline-1 outline-dim" : ""}>
              <TaskCard
                task={t}
                forecast={forecastMap.get(t.id)}
                selected={false}
                onSelect={handleSelect}
                onEdit={handleEdit}
              />
            </div>
          ))
        )}
      </div>

      <TaskForm
        open={formOpen}
        onClose={() => {
          setFormOpen(false);
          setEditingTask(null);
        }}
        editTask={editingTask}
      />

    </div>
  );
}
