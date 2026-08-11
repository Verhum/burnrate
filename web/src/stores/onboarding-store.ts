import { create } from "zustand";
import { TOUR_STEPS } from "@/components/onboarding/tour-steps";

interface OnboardingState {
  open: boolean;
  step: number;
  /** Tab that was active when the tour started, restored when it ends. */
  returnTab: string | null;
  /** Guards the first-open auto-start so it fires at most once per page load. */
  autoStartChecked: boolean;

  start: (returnTab?: string) => void;
  /** Opens the tour once, on the first load where onboarding is not yet done. */
  autoStart: (completed: boolean, returnTab: string) => void;
  next: () => void;
  back: () => void;
  goTo: (step: number) => void;
  close: () => void;
}

export const useOnboardingStore = create<OnboardingState>((set, get) => ({
  open: false,
  step: 0,
  returnTab: null,
  autoStartChecked: false,

  start: (returnTab) =>
    set({
      open: true,
      step: 0,
      returnTab: returnTab ?? null,
      autoStartChecked: true,
    }),

  autoStart: (completed, returnTab) => {
    if (get().autoStartChecked) return;
    set({ autoStartChecked: true });
    if (completed) return;
    set({ open: true, step: 0, returnTab });
  },

  next: () => {
    const { step } = get();
    if (step >= TOUR_STEPS.length - 1) {
      set({ open: false });
      return;
    }
    set({ step: step + 1 });
  },

  back: () => set((s) => ({ step: Math.max(0, s.step - 1) })),

  goTo: (step) =>
    set({ step: Math.min(Math.max(0, step), TOUR_STEPS.length - 1) }),

  close: () => set({ open: false }),
}));
