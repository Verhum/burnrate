"use client";

import type { Task, TaskStatus } from "@/lib/api/types";
import { Button } from "@/components/ui";
import { useTaskStore } from "@/stores/task-store";

/**
 * Status controls for a single task: one or two buttons for the move you
 * actually want from where the task is now, plus a "Move..." select covering
 * everything else.
 *
 * Shared by the task list and the task detail view so the two can never offer
 * different transitions — the detail view used to offer none at all, which meant
 * opening a task to read it and then having to go back to act on it.
 */

/** Every status a user may move a task to, in the order the select lists them. */
const MOVE_TARGETS: TaskStatus[] = [
  "queued",
  "backlog",
  "paused",
  "done",
  "dismissed",
  "failed",
];

interface QuickAction {
  label: string;
  status: TaskStatus;
  variant?: "primary" | "secondary" | "done" | "ghost";
}

/**
 * The one or two moves worth a dedicated button for a task in this status.
 *
 * "Retry"/"Re-queue" resolve to resumable when the task has a session, because
 * the server rejects resumable without one and resuming beats restarting when
 * there is a transcript to carry forward. Either way the move resets the task's
 * attempt count, so a task that gave up at max_attempts gets a full budget back.
 */
function quickActions(task: Task): QuickAction[] {
  switch (task.status) {
    case "queued":
    case "resumable":
      return [{ label: "Pause", status: "paused", variant: "secondary" }];
    case "paused":
      return [
        {
          label: "Resume",
          status: task.has_session ? "resumable" : "queued",
          variant: "primary",
        },
      ];
    case "backlog":
      return [{ label: "Queue", status: "queued", variant: "primary" }];
    case "awaiting_human":
      return [
        { label: "Pause", status: "paused", variant: "secondary" },
      ];
    case "pr_created":
      return [{ label: "Done", status: "done", variant: "done" }];
    case "failed":
      return [
        {
          label: "Retry",
          status: task.has_session ? "resumable" : "queued",
          variant: "primary",
        },
        { label: "Dismiss", status: "dismissed", variant: "ghost" },
      ];
    case "done":
    case "dismissed":
      return [{ label: "Re-queue", status: "queued", variant: "secondary" }];
    default:
      return [];
  }
}

interface TaskStatusControlProps {
  task: Task;
  /** "sm" for the dense task list, "md" for the detail header. */
  size?: "sm" | "md";
  /**
   * Set false where the caller already renders the obvious move itself — the
   * detail view's review banner has its own Done, which also posts the comment
   * typed beside it, and two Done buttons a few pixels apart is a worse offer
   * than one.
   */
  quickActions?: boolean;
}

export function TaskStatusControl({
  task,
  size = "sm",
  quickActions: withQuickActions = true,
}: TaskStatusControlProps) {
  const changeStatus = useTaskStore((s) => s.changeStatus);

  // The server refuses to move a running task, so offering the controls would
  // only produce a 409.
  if (task.status === "running") return null;

  const actions = withQuickActions ? quickActions(task) : [];
  const quickTargets = new Set(actions.map((a) => a.status));
  const targets = MOVE_TARGETS.filter(
    (s) => s !== task.status && !quickTargets.has(s)
  );
  const canResume =
    task.has_session &&
    task.status !== "resumable" &&
    !quickTargets.has("resumable");

  return (
    <>
      {actions.map((a) => (
        <Button
          key={a.status + a.label}
          variant={a.variant ?? "primary"}
          size={size}
          onClick={() => changeStatus(task.id, a.status)}
        >
          {a.label}
        </Button>
      ))}
      {(targets.length > 0 || canResume) && (
        <select
          className={`bg-elevated text-dim font-mono font-bold uppercase tracking-wider
            border-none focus:outline-none cursor-pointer ${
              size === "md" ? "text-[10px] px-2 py-1.5" : "text-[9px] px-1 py-0.5"
            }`}
          value=""
          onChange={(e) => {
            if (e.target.value) {
              changeStatus(task.id, e.target.value as TaskStatus);
            }
          }}
        >
          <option value="" disabled>
            Move...
          </option>
          {targets.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
          {canResume && <option value="resumable">resumable</option>}
        </select>
      )}
    </>
  );
}
