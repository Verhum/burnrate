import { create } from "zustand";
import { client } from "@/lib/api/client";
import { apiErrorMessage } from "@/lib/api/errors";
import { toast } from "@/lib/toast";
import type {
  Task,
  TaskStats,
  CreateTaskRequest,
  UpdateTaskRequest,
  TaskStatus,
} from "@/lib/api/types";

interface TaskState {
  tasks: Task[];
  selectedTask: Task | null;
  pendingLogRunId: number | null;
  filter: string;
  loading: boolean;
  stats: Record<number, TaskStats>;
  _pendingStatuses: Record<number, TaskStatus>;
  /**
   * Task ids with a `run-now` request in flight, so every button that can
   * launch a task disables and reads "Launching…" instead of looking dead
   * while the request is out.
   */
  launching: Record<number, boolean>;

  setFilter: (filter: string) => void;
  selectTask: (task: Task | null, pendingLogRunId?: number) => void;
  clearPendingLogRunId: () => void;
  fetchTasks: () => Promise<void>;
  createTask: (req: CreateTaskRequest) => Promise<Task>;
  updateTask: (id: number, req: UpdateTaskRequest) => Promise<void>;
  deleteTask: (id: number) => Promise<void>;
  reorderTasks: (orderedIds: number[]) => Promise<void>;
  pauseTask: (id: number) => Promise<void>;
  resumeTask: (id: number) => Promise<void>;
  completeTask: (id: number) => Promise<void>;
  dismissTask: (id: number) => Promise<void>;
  runNow: (id: number) => Promise<void>;
  changeStatus: (id: number, status: TaskStatus) => Promise<void>;
  applyTasks: (tasks: Task[]) => void;
}

/** The id the user sees on the card, for toast copy. */
function displayIdOf(tasks: Task[], id: number): string {
  return tasks.find((t) => t.id === id)?.display_id || `BR${id}`;
}

function mergePending(tasks: Task[], pending: Record<number, TaskStatus>): Task[] {
  if (Object.keys(pending).length === 0) return tasks;
  return tasks.map((t) => {
    const ps = pending[t.id];
    return ps ? { ...t, status: ps } : t;
  });
}

export const useTaskStore = create<TaskState>((set, get) => ({
  tasks: [],
  selectedTask: null,
  pendingLogRunId: null,
  filter: "active",
  loading: false,
  stats: {},
  _pendingStatuses: {},
  launching: {},

  setFilter: (filter) => set({ filter }),

  selectTask: (task, pendingLogRunId) => set({ selectedTask: task, pendingLogRunId: pendingLogRunId ?? null }),
  clearPendingLogRunId: () => set({ pendingLogRunId: null }),

  fetchTasks: async () => {
    set({ loading: true });
    try {
      const [tasks, stats] = await Promise.all([
        client.listTasks(),
        client.getTaskStats(),
      ]);
      set({ tasks: mergePending(tasks, get()._pendingStatuses), stats, loading: false });
    } catch {
      set({ loading: false });
    }
  },

  createTask: async (req) => {
    const task = await client.createTask(req);
    await get().fetchTasks();
    return task;
  },

  updateTask: async (id, req) => {
    await client.updateTask(id, req);
    await get().fetchTasks();
  },

  deleteTask: async (id) => {
    try {
      await client.deleteTask(id);
    } catch (err) {
      toast.error(`Couldn't delete ${displayIdOf(get().tasks, id)}`, apiErrorMessage(err));
      return;
    }
    // No success toast: the row disappearing is its own confirmation.
    const { selectedTask } = get();
    if (selectedTask?.id === id) {
      set({ selectedTask: null });
    }
    await get().fetchTasks();
  },

  reorderTasks: async (orderedIds) => {
    await client.reorderTasks({ ordered_ids: orderedIds });
    await get().fetchTasks();
  },

  pauseTask: async (id) => {
    await client.changeTaskStatus(id, { status: "paused" });
    await get().fetchTasks();
  },

  resumeTask: async (id) => {
    await client.changeTaskStatus(id, { status: "resumable" });
    await get().fetchTasks();
  },

  completeTask: async (id) => {
    const { tasks, selectedTask, _pendingStatuses } = get();
    set({
      tasks: tasks.map((t) => (t.id === id ? { ...t, status: "done" as TaskStatus } : t)),
      selectedTask: selectedTask?.id === id ? { ...selectedTask, status: "done" as TaskStatus } : selectedTask,
      _pendingStatuses: { ..._pendingStatuses, [id]: "done" as TaskStatus },
    });
    try {
      await client.changeTaskStatus(id, { status: "done" });
    } finally {
      const { _pendingStatuses: p } = get();
      const { [id]: _, ...remaining } = p;
      set({ _pendingStatuses: remaining });
      await get().fetchTasks();
    }
  },

  dismissTask: async (id) => {
    const { tasks, selectedTask, _pendingStatuses } = get();
    set({
      tasks: tasks.map((t) => (t.id === id ? { ...t, status: "dismissed" as TaskStatus } : t)),
      selectedTask: selectedTask?.id === id ? { ...selectedTask, status: "dismissed" as TaskStatus } : selectedTask,
      _pendingStatuses: { ..._pendingStatuses, [id]: "dismissed" as TaskStatus },
    });
    try {
      await client.changeTaskStatus(id, { status: "dismissed" });
    } finally {
      const { _pendingStatuses: p } = get();
      const { [id]: _, ...remaining } = p;
      set({ _pendingStatuses: remaining });
      await get().fetchTasks();
    }
  },

  runNow: async (id) => {
    if (get().launching[id]) return;
    const displayId = displayIdOf(get().tasks, id);
    set({ launching: { ...get().launching, [id]: true } });
    try {
      await client.runTaskNow(id);
      // The run takes a moment to appear, so without this the button that just
      // worked is indistinguishable from one that did nothing.
      toast.success(`${displayId} launched`);
    } catch (err) {
      // Scheduler.RunNow has seven refusal reasons, all 409s, and every one of
      // them is the answer to "why isn't it doing anything?".
      toast.error(`Couldn't run ${displayId}`, apiErrorMessage(err));
    } finally {
      const launching = { ...get().launching };
      delete launching[id];
      set({ launching });
      await get().fetchTasks();
    }
  },

  changeStatus: async (id, status) => {
    const { tasks, selectedTask, _pendingStatuses } = get();
    set({
      tasks: tasks.map((t) => (t.id === id ? { ...t, status } : t)),
      selectedTask: selectedTask?.id === id ? { ...selectedTask, status } : selectedTask,
      _pendingStatuses: { ..._pendingStatuses, [id]: status },
    });
    try {
      await client.changeTaskStatus(id, { status });
    } finally {
      const { _pendingStatuses: p } = get();
      const { [id]: _, ...remaining } = p;
      set({ _pendingStatuses: remaining });
      await get().fetchTasks();
    }
  },

  applyTasks: (tasks) => {
    const { selectedTask, _pendingStatuses } = get();
    const merged = mergePending(tasks, _pendingStatuses);
    const updated = selectedTask
      ? merged.find((t) => t.id === selectedTask.id) ?? null
      : null;
    set({ tasks: merged, selectedTask: updated });
  },
}));
