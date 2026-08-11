"use client";

import { useToastStore, type Toast, type ToastKind } from "@/stores/toast-store";

const KIND_COLOR: Record<ToastKind, string> = {
  success: "text-sage",
  error: "text-danger",
  info: "text-amber",
};

const KIND_LABEL: Record<ToastKind, string> = {
  success: "Done",
  error: "Error",
  info: "Note",
};

function ToastItem({ item }: { item: Toast }) {
  const dismiss = useToastStore((s) => s.dismiss);
  const isError = item.kind === "error";

  return (
    <div
      role={isError ? "alert" : "status"}
      aria-live={isError ? "assertive" : "polite"}
      className="pointer-events-auto w-[320px] max-w-[calc(100vw-2rem)] bg-elevated px-3 py-2 flex items-start gap-2"
    >
      <div className="flex-1 min-w-0">
        <div className="flex items-baseline gap-1.5">
          <span
            className={`text-[9px] font-bold uppercase tracking-widest font-mono ${KIND_COLOR[item.kind]}`}
          >
            {KIND_LABEL[item.kind]}
          </span>
          {item.count > 1 && (
            <span className="text-[9px] font-mono text-muted">×{item.count}</span>
          )}
        </div>
        <p className="text-[11px] text-primary font-mono mt-0.5 break-words">
          {item.title}
        </p>
        {item.message && (
          <p className="text-[10px] text-dim font-mono mt-1 break-words whitespace-pre-wrap">
            {item.message}
          </p>
        )}
      </div>
      <button
        onClick={() => dismiss(item.id)}
        aria-label={`Dismiss notification: ${item.title}`}
        className="text-muted hover:text-dim text-sm leading-none cursor-pointer bg-transparent border-none font-mono"
      >
        ×
      </button>
    </div>
  );
}

/**
 * The app's toast container. Mount exactly once, in `AppShell`.
 *
 * `z-[60]` rather than `z-50`: `Modal` is `z-50` and renders inline instead of
 * through a portal, so an equal z-index would leave the stacking order to DOM
 * order — and a failure raised from inside a dialog has to stay visible.
 */
export function Toaster() {
  const toasts = useToastStore((s) => s.toasts);

  return (
    <div
      aria-live="polite"
      className="fixed bottom-4 right-4 z-[60] flex flex-col items-end gap-2 pointer-events-none"
    >
      {toasts.map((t) => (
        <ToastItem key={t.id} item={t} />
      ))}
    </div>
  );
}
