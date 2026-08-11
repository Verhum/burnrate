import { create } from "zustand";
import { client } from "@/lib/api/client";
import type { HumanRequest } from "@/lib/api/types";

interface RequestState {
  /**
   * Every pending human request, across all tasks — the same payload as
   * `GET /api/requests?status=pending` and the SSE `request` event. Views filter
   * it themselves; never filter inside a selector, or zustand's Object.is
   * snapshot comparison sees a fresh array on every read (see
   * `lib/selector-stability.test.ts`).
   */
  pending: HumanRequest[];
  loading: boolean;
  /**
   * Bumped whenever the pending set is replaced. Views that hold data derived
   * from a request — the comment thread, which gains a comment when someone
   * responds — watch this instead of polling.
   */
  revision: number;

  fetchRequests: () => Promise<void>;
  applyRequests: (requests: HumanRequest[]) => void;
  clear: () => void;
}

export const useRequestStore = create<RequestState>((set, get) => ({
  pending: [],
  loading: false,
  revision: 0,

  fetchRequests: async () => {
    set({ loading: true });
    try {
      const pending = await client.listRequests("pending");
      set({ pending, loading: false, revision: get().revision + 1 });
    } catch {
      // A failed poll is not worth a toast: the SSE stream and the next focus
      // both retry, and the banner simply keeps its last known state.
      set({ loading: false });
    }
  },

  applyRequests: (requests) =>
    set({ pending: requests, loading: false, revision: get().revision + 1 }),

  clear: () => set({ pending: [], revision: get().revision + 1 }),
}));
