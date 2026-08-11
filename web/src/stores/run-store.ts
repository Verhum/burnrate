import { create } from "zustand";
import { client } from "@/lib/api/client";
import { apiErrorMessage } from "@/lib/api/errors";
import { toast } from "@/lib/toast";
import type { Run, LogEvent } from "@/lib/api/types";

interface RunState {
  runs: Run[];
  activeTaskId?: number;
  activeRunLog: string;
  activeRunEvents: LogEvent[];
  loading: boolean;

  fetchRuns: (taskId?: number) => Promise<void>;
  cancelRun: (id: number) => Promise<void>;
  cancelActiveRunForTask: (taskId: number) => Promise<void>;
  fetchLog: (runId: number) => Promise<void>;
  fetchEvents: (runId: number) => Promise<void>;
  applyRuns: (runs: Run[]) => void;
  clearActiveLog: () => void;
}

export const useRunStore = create<RunState>((set, get) => ({
  runs: [],
  activeTaskId: undefined,
  activeRunLog: "",
  activeRunEvents: [],
  loading: false,

  fetchRuns: async (taskId?) => {
    set({ loading: true, activeTaskId: taskId });
    try {
      const runs = await client.listRuns({
        limit: 50,
        taskId,
      });
      set({ runs, loading: false });
    } catch {
      set({ loading: false });
    }
  },

  cancelRun: async (id) => {
    try {
      await client.cancelRun(id);
    } catch (err) {
      toast.error(`Couldn't cancel run #${id}`, apiErrorMessage(err));
    }
  },

  cancelActiveRunForTask: async (taskId) => {
    try {
      const runs = await client.listRuns({ taskId, limit: 10 });
      const active = runs.find(
        (r) => r.status === "starting" || r.status === "running" || r.status === "resuming"
      );
      if (!active) {
        toast.info("No active run", "This task doesn't have a running agent to cancel.");
        return;
      }
      await client.cancelRun(active.id);
    } catch (err) {
      toast.error("Couldn't cancel run", apiErrorMessage(err));
    }
  },

  fetchLog: async (runId) => {
    const log = await client.getRunLog(runId);
    set({ activeRunLog: log });
  },

  fetchEvents: async (runId) => {
    const events = await client.getRunEvents(runId);
    set({ activeRunEvents: events });
  },

  applyRuns: (runs) => {
    const { activeTaskId } = get();
    if (activeTaskId !== undefined) {
      set({ runs: runs.filter((r) => r.task_id === activeTaskId) });
    } else {
      set({ runs });
    }
  },

  clearActiveLog: () => set({ activeRunLog: "", activeRunEvents: [] }),
}));
