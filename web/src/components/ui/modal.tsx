"use client";

import { useEffect, useRef, type ReactNode } from "react";

type ModalSize = "sm" | "lg";

interface ModalProps {
  open: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
  actions?: ReactNode;
  /** `lg` (default) fits the task form; `sm` fits a confirmation. */
  size?: ModalSize;
}

const sizeClasses: Record<ModalSize, string> = {
  sm: "max-w-md",
  lg: "max-w-5xl",
};

/**
 * Escape closes only the topmost Modal. `ConfirmDialog` nests inside the
 * run-log Modal, and with a per-modal document listener one keypress would
 * otherwise dismiss the confirmation *and* the dialog behind it.
 */
const escapeStack: object[] = [];

/**
 * Whether any `Modal` is currently open. `use-keyboard` consults this so its
 * global shortcuts stand down for a dialog — otherwise its `Enter` handler
 * preventDefaults the keypress that would activate `ConfirmDialog`'s focused
 * button, and its `Escape` handler navigates the view behind the dialog.
 */
export function hasOpenModal(): boolean {
  return escapeStack.length > 0;
}

export function Modal({
  open,
  onClose,
  title,
  children,
  actions,
  size = "lg",
}: ModalProps) {
  // Held in a ref so the stack effect below can depend on `open` alone; call
  // sites pass an inline arrow, which would otherwise re-register (and reorder)
  // the listener on every render.
  const onCloseRef = useRef(onClose);
  useEffect(() => {
    onCloseRef.current = onClose;
  }, [onClose]);

  useEffect(() => {
    if (!open) return;
    const token = {};
    escapeStack.push(token);
    const handler = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      if (escapeStack[escapeStack.length - 1] !== token) return;
      onCloseRef.current();
    };
    document.addEventListener("keydown", handler);
    return () => {
      document.removeEventListener("keydown", handler);
      const i = escapeStack.lastIndexOf(token);
      if (i !== -1) escapeStack.splice(i, 1);
    };
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = prev;
    };
  }, [open]);

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/60" onClick={onClose} />
      <div
        className={`relative bg-surface w-full ${sizeClasses[size]} mx-4 max-h-[90vh] flex flex-col`}
      >
        <div className="px-6 py-4 flex items-center justify-between">
          <h2 className="text-sm font-bold uppercase tracking-wider text-dim">{title}</h2>
          <button
            onClick={onClose}
            className="text-muted hover:text-dim text-lg cursor-pointer bg-transparent border-none font-mono"
          >
            ×
          </button>
        </div>
        <div className="px-6 py-4 overflow-y-auto overscroll-contain flex-1">{children}</div>
        {actions && <div className="px-6 py-4 flex gap-2 justify-end">{actions}</div>}
      </div>
    </div>
  );
}
