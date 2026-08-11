import { create } from "zustand";

export type ToastKind = "success" | "error" | "info";

export interface Toast {
  id: number;
  kind: ToastKind;
  /** Short headline, e.g. "Couldn't run BR72". */
  title: string;
  /** The detail, e.g. the scheduler's refusal reason. */
  message?: string;
  /** >1 when an identical toast was raised again and coalesced. */
  count: number;
}

interface ToastState {
  toasts: Toast[];
  push: (kind: ToastKind, title: string, message?: string) => number;
  dismiss: (id: number) => void;
  clear: () => void;
}

/**
 * Per-kind lifetime in ms. `0` means "stays until dismissed": an error the user
 * cannot finish reading is the bug this channel exists to fix — the scheduler's
 * refusal reasons are a sentence long and are the whole answer to "why isn't it
 * doing anything?".
 */
const LIFETIME_MS: Record<ToastKind, number> = {
  success: 3000,
  info: 5000,
  error: 0,
};

/** A burst must not cover the UI; oldest is dropped first. */
const MAX_VISIBLE = 4;

const timers = new Map<number, ReturnType<typeof setTimeout>>();
let nextId = 1;

function clearTimer(id: number) {
  const t = timers.get(id);
  if (t !== undefined) {
    clearTimeout(t);
    timers.delete(id);
  }
}

/**
 * (Re)arm the auto-dismiss timer. Always clears the previous one first so a
 * manually dismissed toast can't be re-dismissed by its own stale timer, and so
 * a coalesced repeat restarts the countdown rather than stacking timers.
 */
function arm(id: number, kind: ToastKind) {
  clearTimer(id);
  const ms = LIFETIME_MS[kind];
  if (ms <= 0) return;
  timers.set(
    id,
    setTimeout(() => {
      timers.delete(id);
      useToastStore.getState().dismiss(id);
    }, ms)
  );
}

export const useToastStore = create<ToastState>((set, get) => ({
  toasts: [],

  push: (kind, title, message) => {
    const { toasts } = get();
    const existing = toasts.find(
      (t) => t.kind === kind && t.title === title && t.message === message
    );
    if (existing) {
      set({
        toasts: toasts.map((t) =>
          t.id === existing.id ? { ...t, count: t.count + 1 } : t
        ),
      });
      arm(existing.id, kind);
      return existing.id;
    }

    const id = nextId++;
    const next = [...toasts, { id, kind, title, message, count: 1 }];
    while (next.length > MAX_VISIBLE) {
      const dropped = next.shift();
      if (dropped) clearTimer(dropped.id);
    }
    set({ toasts: next });
    arm(id, kind);
    return id;
  },

  dismiss: (id) => {
    clearTimer(id);
    set({ toasts: get().toasts.filter((t) => t.id !== id) });
  },

  clear: () => {
    for (const t of get().toasts) clearTimer(t.id);
    set({ toasts: [] });
  },
}));
