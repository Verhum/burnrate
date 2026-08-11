import { create } from "zustand";
import { client } from "@/lib/api/client";
import { useTaskStore } from "./task-store";
import { useRunStore } from "./run-store";
import { useUsageStore } from "./usage-store";
import { useRequestStore } from "./request-store";
import type {
  UsageSnapshot,
  StatusInfo,
  Task,
  Run,
  HumanRequest,
} from "@/lib/api/types";

interface SSEState {
  connected: boolean;
  connectionStatus: "disconnected" | "live" | "reconnecting";

  connect: () => void;
  disconnect: () => void;
}

const MAX_BACKOFF = 30_000;
const INITIAL_BACKOFF = 1_000;

let eventSource: EventSource | null = null;
let backoff = INITIAL_BACKOFF;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;

function clearReconnectTimer() {
  if (reconnectTimer !== null) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
}

export const useSSEStore = create<SSEState>((set, get) => ({
  connected: false,
  connectionStatus: "disconnected",

  connect: () => {
    if (eventSource) {
      eventSource.close();
    }
    clearReconnectTimer();

    const es = client.connectSSE();
    eventSource = es;

    es.onopen = () => {
      backoff = INITIAL_BACKOFF;
      set({ connected: true, connectionStatus: "live" });
    };

    es.addEventListener("usage", (e: MessageEvent) => {
      const data = JSON.parse(e.data) as UsageSnapshot;
      useUsageStore.getState().applyUsage(data);
    });

    es.addEventListener("status", (e: MessageEvent) => {
      const data = JSON.parse(e.data) as StatusInfo;
      useUsageStore.getState().applyStatus(data);
    });

    es.addEventListener("caffeinate", (_e: MessageEvent) => {
      // CaffeinateStatus events are received but caffeinate state
      // is not currently tracked in a store — extend if needed
      void (_e.data as string satisfies string);
    });

    es.addEventListener("voice-open", () => {
      window.dispatchEvent(new CustomEvent("burnrate-voice-open"));
    });

    es.addEventListener("task", (e: MessageEvent) => {
      const data = JSON.parse(e.data) as Task[] | Task;
      if (Array.isArray(data)) {
        useTaskStore.getState().applyTasks(data);
      } else {
        useTaskStore.getState().fetchTasks();
      }
    });

    es.addEventListener("run", (e: MessageEvent) => {
      const data = JSON.parse(e.data) as Run[] | Run;
      if (Array.isArray(data)) {
        useRunStore.getState().applyRuns(data);
      } else {
        const { activeTaskId } = useRunStore.getState();
        useRunStore.getState().fetchRuns(activeTaskId);
      }
    });

    // The payload is the full pending set, pushed on create, respond, deny and
    // cancel alike. Applying it directly is what makes a request raised while
    // the task is still `running` show up before the agent's long poll expires.
    es.addEventListener("request", (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data) as HumanRequest[] | unknown;
        if (Array.isArray(data)) {
          useRequestStore.getState().applyRequests(data as HumanRequest[]);
        } else {
          useRequestStore.getState().fetchRequests();
        }
      } catch {
        useRequestStore.getState().fetchRequests();
      }
      // Status badges (`awaiting_human` → `resumable`) move with the request.
      useTaskStore.getState().fetchTasks();
    });

    es.onerror = () => {
      set({ connectionStatus: "reconnecting" });
      es.close();
      eventSource = null;

      clearReconnectTimer();
      reconnectTimer = setTimeout(() => {
        backoff = Math.min(backoff * 2, MAX_BACKOFF);
        get().connect();
      }, backoff);
    };
  },

  disconnect: () => {
    clearReconnectTimer();
    if (eventSource) {
      eventSource.close();
      eventSource = null;
    }
    set({ connected: false, connectionStatus: "disconnected" });
  },
}));
