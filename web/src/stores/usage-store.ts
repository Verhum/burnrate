import { create } from "zustand";
import { client } from "@/lib/api/client";
import type { UsageSnapshot, StatusInfo } from "@/lib/api/types";

interface UsageState {
  usage: UsageSnapshot | null;
  status: StatusInfo | null;

  fetchUsage: () => Promise<void>;
  fetchStatus: () => Promise<void>;
  applyUsage: (usage: UsageSnapshot) => void;
  applyStatus: (status: StatusInfo) => void;
  clear: () => void;
}

export const useUsageStore = create<UsageState>((set) => ({
  usage: null,
  status: null,

  fetchUsage: async () => {
    try {
      const usage = await client.getUsage();
      set({ usage });
    } catch {
      // ignore
    }
  },

  fetchStatus: async () => {
    try {
      const status = await client.getStatus();
      set({ status });
    } catch {
      // ignore
    }
  },

  applyUsage: (usage) => set({ usage }),
  applyStatus: (status) => set({ status }),
  clear: () => set({ usage: null, status: null }),
}));
