"use client";

import { useEffect, useCallback } from "react";
import { hasOpenModal } from "@/components/ui/modal";

export interface KeyboardActions {
  onNewTask: () => void;
  onNavigateUp: () => void;
  onNavigateDown: () => void;
  onOpenSelected: () => void;
  onGoBack: () => void;
  onSwitchTab: (tab: string) => void;
  onSetFilter: (filter: string) => void;
  onSetStatus: (status: string) => void;
  onRunNow: () => void;
  onToggleHelp: () => void;
  hasSelectedTask: boolean;
  hasDetailView: boolean;
  /** Suppress all global shortcuts (e.g. while the tutorial owns the keyboard). */
  disabled?: boolean;
}

const TAB_MAP: Record<string, string> = {
  "1": "tasks",
  "2": "runs",
  "3": "usage",
  "4": "config",
};

const FILTER_KEYS: Record<string, string> = {
  a: "active",
  c: "completed",
  f: "failed",
};

const STATUS_KEYS: Record<string, string> = {
  q: "queued",
  d: "done",
  x: "dismissed",
  b: "backlog",
};

function isInputFocused(): boolean {
  const el = document.activeElement;
  if (!el) return false;
  const tag = el.tagName.toLowerCase();
  return (
    tag === "input" ||
    tag === "textarea" ||
    tag === "select" ||
    (el as HTMLElement).isContentEditable
  );
}

export function useKeyboard(actions: KeyboardActions) {
  const handler = useCallback(
    (e: KeyboardEvent) => {
      if (actions.disabled) return;

      // Cmd/Ctrl+Enter: submit (handled by form components, but we can use it)
      if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
        const submit = document.querySelector<HTMLButtonElement>(
          "[data-keyboard-submit]"
        );
        if (submit) {
          e.preventDefault();
          submit.click();
          return;
        }
      }

      // A Modal owns the keyboard while it is open: otherwise Enter here
      // preventDefaults the keypress that would activate ConfirmDialog's
      // focused confirm button, and Escape navigates the view behind it in
      // addition to closing the dialog.
      if (hasOpenModal()) return;

      // Don't intercept when typing in inputs
      if (isInputFocused()) return;

      // Escape: close modal / go back
      if (e.key === "Escape") {
        e.preventDefault();
        actions.onGoBack();
        return;
      }

      // ?: toggle help
      if (e.key === "?") {
        e.preventDefault();
        actions.onToggleHelp();
        return;
      }

      // n: new task
      if (e.key === "n") {
        e.preventDefault();
        actions.onNewTask();
        return;
      }

      // j/k or arrow up/down: navigate
      if (e.key === "j" || e.key === "ArrowDown") {
        e.preventDefault();
        actions.onNavigateDown();
        return;
      }
      if (e.key === "k" || e.key === "ArrowUp") {
        e.preventDefault();
        actions.onNavigateUp();
        return;
      }

      // Enter: open selected task
      if (e.key === "Enter") {
        e.preventDefault();
        actions.onOpenSelected();
        return;
      }

      // Tab keys: 1-4 switch main tabs
      if (TAB_MAP[e.key]) {
        e.preventDefault();
        actions.onSwitchTab(TAB_MAP[e.key]);
        return;
      }

      // Filter keys (only when in tasks tab, no detail)
      if (!actions.hasDetailView && FILTER_KEYS[e.key]) {
        e.preventDefault();
        actions.onSetFilter(FILTER_KEYS[e.key]);
        return;
      }

      // Status keys (only when viewing task detail)
      if (actions.hasDetailView && STATUS_KEYS[e.key]) {
        e.preventDefault();
        actions.onSetStatus(STATUS_KEYS[e.key]);
        return;
      }

      // r: run now (only in detail view)
      if (actions.hasDetailView && e.key === "r") {
        e.preventDefault();
        actions.onRunNow();
        return;
      }
    },
    [actions]
  );

  useEffect(() => {
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [handler]);
}
